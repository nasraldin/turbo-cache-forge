package migrations_test

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db/migrations"
)

func TestSQLiteMigrationsApply(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqlDB, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	sub, err := fs.Sub(migrations.FS, "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, sqlDB, sub)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Up(context.Background()); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	// every table the repo uses must now exist
	for _, tbl := range []string{"organizations", "projects", "api_keys", "cache_artifacts", "usage_daily"} {
		var name string
		err := sqlDB.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name)
		if err != nil {
			t.Fatalf("table %q missing after migrate: %v", tbl, err)
		}
	}
	// read_only column from 003 must exist
	var ro int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('api_keys') WHERE name='read_only'`).Scan(&ro); err != nil || ro != 1 {
		t.Fatalf("read_only column missing: count=%d err=%v", ro, err)
	}
}
