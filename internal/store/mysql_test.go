package store

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

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
