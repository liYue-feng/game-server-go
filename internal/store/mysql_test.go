package store

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestUnparameterizedGORMLoggerInterpolatesSensitiveValues(t *testing.T) {
	const secret = "secret-open-id-and-token"
	var queryLog bytes.Buffer
	unsafeLogger := gormlogger.New(log.New(&queryLog, "", 0), gormlogger.Config{
		LogLevel: gormlogger.Error,
	})

	traceGORMError(t, unsafeLogger, secret)

	if !strings.Contains(queryLog.String(), secret) {
		t.Fatalf("unparameterized GORM log = %q, want proof that sensitive value is interpolated", queryLog.String())
	}
}

func TestMySQLGORMLoggerKeepsSensitiveValuesParameterized(t *testing.T) {
	const secret = "secret-open-id-and-token"
	var queryLog bytes.Buffer
	productionLogger := newMySQLGORMLogger(log.New(&queryLog, "", 0))

	renderedSQL := traceGORMError(t, productionLogger, secret)
	logOutput := queryLog.String()

	if strings.Contains(renderedSQL, secret) || strings.Contains(logOutput, secret) {
		t.Fatalf("parameterized GORM output exposed secret: sql=%q log=%q", renderedSQL, logOutput)
	}
	if !strings.Contains(renderedSQL, "open_id = ?") || !strings.Contains(renderedSQL, "token = ?") {
		t.Fatalf("parameterized SQL = %q, want diagnostic statement with placeholders", renderedSQL)
	}
	if !strings.Contains(logOutput, "query failed") || !strings.Contains(logOutput, "SELECT * FROM players") {
		t.Fatalf("GORM error log = %q, want error and SQL diagnostic", logOutput)
	}
}

func traceGORMError(t *testing.T, sqlLogger gormlogger.Interface, secret string) string {
	t.Helper()
	const statement = "SELECT * FROM players WHERE open_id = ? AND token = ?"
	filter, ok := sqlLogger.(interface {
		ParamsFilter(context.Context, string, ...interface{}) (string, []interface{})
	})
	if !ok {
		t.Fatal("GORM logger does not implement ParamsFilter")
	}
	filteredSQL, filteredParams := filter.ParamsFilter(context.Background(), statement, secret, secret)
	dialector := mysql.New(mysql.Config{SkipInitializeWithVersion: true})
	renderedSQL := dialector.Explain(filteredSQL, filteredParams...)
	sqlLogger.Trace(context.Background(), time.Now(), func() (string, int64) {
		return renderedSQL, 0
	}, errors.New("query failed"))
	return renderedSQL
}

func TestValidateAndCloseOnFailureClosesMySQLOnceAndPreservesMigrationError(t *testing.T) {
	migrationErr := errors.New("migration failed")
	closeErr := errors.New("close failed")
	closeCalls := 0

	err := validateAndCloseOnFailure(
		func() error { return migrationErr },
		func() error {
			closeCalls++
			return closeErr
		},
	)

	if err != migrationErr {
		t.Fatalf("validateAndCloseOnFailure() error = %v, want original migration error %v", err, migrationErr)
	}
	if closeCalls != 1 {
		t.Fatalf("validateAndCloseOnFailure() close calls = %d, want 1", closeCalls)
	}
}

func TestSaveArchiveBuildsAtomicPlayerIDUpsert(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "root:@tcp(127.0.0.1:3306)/game_db?parseTime=true",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DryRun:                 true,
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	statement := saveArchiveQuery(db, &Archive{ID: 777, PlayerID: 42, Data: `{"gold":7}`})
	if statement.Error != nil {
		t.Fatalf("saveArchiveQuery() error = %v", statement.Error)
	}

	sql := strings.ToLower(statement.Statement.SQL.String())
	if !strings.Contains(sql, "insert into") || !strings.Contains(sql, "on duplicate key update") {
		t.Fatalf("SaveArchive SQL = %q, want atomic insert upsert", sql)
	}
	if !strings.Contains(sql, "`data`=values(`data`)") {
		t.Fatalf("SaveArchive SQL = %q, want data updated from inserted value", sql)
	}
	if !strings.Contains(sql, "`updated_at`=values(`updated_at`)") {
		t.Fatalf("SaveArchive SQL = %q, want updated_at refreshed on conflict", sql)
	}
	if strings.Contains(sql, "where `id`") {
		t.Fatalf("SaveArchive SQL = %q, must not select/update by zero primary key", sql)
	}
	if strings.Contains(sql, "`id`") {
		t.Fatalf("SaveArchive SQL = %q, must ignore caller-provided archive ID", sql)
	}

	conflictClause := sql[strings.Index(sql, "on duplicate key update"):]
	if strings.Count(conflictClause, "=values(") != 2 {
		t.Fatalf("SaveArchive conflict clause = %q, want only data and updated_at assignments", conflictClause)
	}
}

func TestMySQLStoreUpdatePlayerReturnsNotFoundWithoutInsert(t *testing.T) {
	var queryLog bytes.Buffer
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "root:@tcp(127.0.0.1:3306)/game_db?parseTime=true",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DryRun:                 true,
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger: gormlogger.New(log.New(&queryLog, "", 0), gormlogger.Config{
			LogLevel: gormlogger.Info,
		}),
	})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	developmentStore := &MySQLStore{db: db}
	err = developmentStore.UpdatePlayer(&Player{ID: 404, OpenID: "dev:missing", Nickname: "Missing"})
	if !IsNotFound(err) {
		t.Fatalf("UpdatePlayer(missing) error = %v, want not found", err)
	}

	sql := strings.ToLower(queryLog.String())
	if !strings.Contains(sql, "update `players`") || !strings.Contains(sql, "where id =") {
		t.Fatalf("UpdatePlayer SQL = %q, want conditional update by ID", sql)
	}
	if strings.Contains(sql, "insert into `players`") {
		t.Fatalf("UpdatePlayer SQL = %q, must not insert a missing player", sql)
	}
}
