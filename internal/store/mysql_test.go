package store

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"game-server/internal/protocolpb"

	"github.com/DATA-DOG/go-sqlmock"
	drivermysql "github.com/go-sql-driver/mysql"
	"google.golang.org/protobuf/proto"
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

func TestMySQLSettlementDuplicateConflictRollsBackAndReturnsStoredSnapshot(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	repository := NewMySQLCombatSettlementRepository(&MySQLStore{db: db}, CombatRewardPolicy{GoldPerKill: 5, ExpPerKill: 10})
	req := settlementRequest("duplicate-mysql-run", protocolpb.BattleOutcome_BATTLE_OUTCOME_VICTORY)
	stored := &protocolpb.CombatResultResp{
		Success: true, RewardGold: 20, RewardExp: 40, BestScore: 321,
		Archive: &protocolpb.PlayerArchive{SchemaVersion: 1, Gold: 20, Exp: 40, BestScore: 321, TotalKills: 4, TotalGames: 1, HighestClearedDungeon: 3, LastStyleId: 3},
	}
	storedBytes, err := proto.Marshal(stored)
	if err != nil {
		t.Fatalf("marshal stored response: %v", err)
	}

	mock.ExpectBegin()
	// This first business query must retain FOR UPDATE; without it, the same
	// player's different runs can interleave under READ COMMITTED.
	mock.ExpectQuery("SELECT \\* FROM `players`.*FOR UPDATE").
		WithArgs(int64(21), 1).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(21))
	mock.ExpectQuery("SELECT \\* FROM `combat_settlements`").
		WithArgs(int64(21), req.RunId, 1).WillReturnRows(sqlmock.NewRows([]string{"id", "player_id", "run_id", "response", "created_at"}))
	mock.ExpectQuery("SELECT \\* FROM `archives`").
		WithArgs(int64(21), 1).WillReturnRows(sqlmock.NewRows([]string{"id", "player_id", "data", "created_at", "updated_at"}))
	mock.ExpectQuery("SELECT \\* FROM `player_stats`").
		WithArgs(int64(21), 1).WillReturnRows(sqlmock.NewRows([]string{"id", "player_id"}))
	mock.ExpectExec("INSERT INTO `player_stats`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO `score_records`").
		WithArgs(int64(21), int64(321), `{"kills":4,"survival_time":42.5,"dungeon_level":3,"style_id":3}`, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO `archives`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO `combat_settlements`").WillReturnError(&drivermysql.MySQLError{Number: 1062, Message: "duplicate"})
	mock.ExpectRollback()
	mock.ExpectQuery("SELECT \\* FROM `combat_settlements`").
		WithArgs(int64(21), req.RunId, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "player_id", "run_id", "response", "created_at"}).AddRow(1, 21, req.RunId, storedBytes, time.Now()))

	response, err := repository.Settle(21, req)
	if err != nil {
		t.Fatalf("Settle() error = %v", err)
	}
	if !response.Duplicate {
		t.Fatal("duplicate response has Duplicate = false, want true")
	}
	if response.RunId != req.RunId {
		t.Fatalf("stored duplicate run ID = %q, want %q", response.RunId, req.RunId)
	}
	want := proto.Clone(stored).(*protocolpb.CombatResultResp)
	want.Duplicate = true
	want.RunId = req.RunId
	if !proto.Equal(response, want) {
		t.Fatalf("stored snapshot changed: got %v want %v", response, stored)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("MySQL transaction expectations: %v", err)
	}
}

func TestMySQLSettlementExistingDuplicateBackfillsRunIDFromLegacySnapshot(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	repository := NewMySQLCombatSettlementRepository(&MySQLStore{db: db}, CombatRewardPolicy{GoldPerKill: 5, ExpPerKill: 10})
	req := settlementRequest("legacy-existing-mysql-run", protocolpb.BattleOutcome_BATTLE_OUTCOME_VICTORY)
	legacy := &protocolpb.CombatResultResp{
		Success: true, RewardGold: 20, RewardExp: 40, BestScore: 321,
		Archive: &protocolpb.PlayerArchive{SchemaVersion: 1, Gold: 20, Exp: 40},
	}
	legacyBytes, err := proto.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy response: %v", err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT \\* FROM `players`.*FOR UPDATE").
		WithArgs(int64(21), 1).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(21))
	mock.ExpectQuery("SELECT \\* FROM `combat_settlements`").
		WithArgs(int64(21), req.RunId, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "player_id", "run_id", "response", "created_at"}).AddRow(1, 21, req.RunId, legacyBytes, time.Now()))
	mock.ExpectCommit()

	response, err := repository.Settle(21, req)
	if err != nil {
		t.Fatalf("Settle() error = %v", err)
	}
	if !response.Duplicate {
		t.Fatal("legacy duplicate response has Duplicate = false, want true")
	}
	if response.RunId != req.RunId {
		t.Fatalf("legacy duplicate run ID = %q, want current request %q", response.RunId, req.RunId)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("MySQL transaction expectations: %v", err)
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

	protobufBytes := []byte{0x08, 0x01, 0x10, 0x07, 0x4a, 0x02, 0x01, 0x03}
	statement := saveArchiveQuery(db, &Archive{ID: 777, PlayerID: 42, Data: protobufBytes})
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
	if len(statement.Statement.Vars) == 0 {
		t.Fatal("SaveArchive query has no bound values")
	}
	var boundProtobufBytes []byte
	for _, value := range statement.Statement.Vars {
		if data, ok := value.([]byte); ok && bytes.Equal(data, protobufBytes) {
			boundProtobufBytes = data
			break
		}
	}
	if boundProtobufBytes == nil {
		t.Fatalf("SaveArchive bindings = %#v, want protobuf bytes %#v", statement.Statement.Vars, protobufBytes)
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
