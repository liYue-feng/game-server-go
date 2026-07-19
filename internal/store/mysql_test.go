package store

import (
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
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

	statement := saveArchiveQuery(db, &Archive{PlayerID: 42, Data: `{"gold":7}`})
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
}
