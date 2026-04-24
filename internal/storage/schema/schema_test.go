package schema

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

type fakeMigrationDB struct {
	fail func(query string) error
	seen []string
}

func (db *fakeMigrationDB) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	db.seen = append(db.seen, query)
	if db.fail != nil {
		if err := db.fail(query); err != nil {
			return nil, err
		}
	}
	return fakeSQLResult(0), nil
}

func (db *fakeMigrationDB) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("fakeMigrationDB.QueryContext should not be called")
}

func (db *fakeMigrationDB) QueryRowContext(context.Context, string, ...any) *sql.Row {
	return nil
}

type fakeSQLResult int64

func (r fakeSQLResult) LastInsertId() (int64, error) {
	return int64(r), nil
}

func (r fakeSQLResult) RowsAffected() (int64, error) {
	return int64(r), nil
}

func TestRunMigrationsRejectsNonConcurrentStatementError(t *testing.T) {
	db := &fakeMigrationDB{
		fail: func(query string) error {
			if strings.Contains(query, "CREATE TABLE IF NOT EXISTS issues") {
				return errors.New("synthetic DDL failure")
			}
			return nil
		},
	}

	applied, err := runMigrations(context.Background(), db, 0)
	if err == nil {
		t.Fatal("runMigrations should reject non-concurrent DDL errors")
	}
	if applied != 0 {
		t.Fatalf("applied = %d, want 0", applied)
	}
	if !strings.Contains(err.Error(), "0001_create_issues.up.sql") || !strings.Contains(err.Error(), "synthetic DDL failure") {
		t.Fatalf("error = %v, want migration filename and DDL failure", err)
	}
	for _, query := range db.seen {
		if strings.Contains(query, "INSERT IGNORE INTO schema_migrations") {
			t.Fatalf("schema_migrations should not be recorded after failed statement; saw %q", query)
		}
	}
}

func TestRunMigrationsToleratesConcurrentStatementError(t *testing.T) {
	var failed bool
	db := &fakeMigrationDB{
		fail: func(query string) error {
			if !failed && strings.Contains(query, "CREATE TABLE IF NOT EXISTS issues") {
				failed = true
				return errors.New("table already exists")
			}
			return nil
		},
	}

	applied, err := runMigrations(context.Background(), db, 0)
	if err != nil {
		t.Fatalf("runMigrations should tolerate already-exists races: %v", err)
	}
	if applied != LatestVersion() {
		t.Fatalf("applied = %d, want %d", applied, LatestVersion())
	}
	if !failed {
		t.Fatal("test did not inject the expected already-exists error")
	}
}

func TestRunMigrationsExecutesMultiStatementMigrationIndividually(t *testing.T) {
	var sawIssuesStartedAt bool
	var sawWispsStartedAt bool
	db := &fakeMigrationDB{
		fail: func(query string) error {
			if strings.Contains(query, "ALTER TABLE issues ADD COLUMN started_at") {
				sawIssuesStartedAt = true
			}
			if strings.Contains(query, "ALTER TABLE wisps ADD COLUMN started_at") {
				sawWispsStartedAt = true
				return errors.New("duplicate column name 'started_at'")
			}
			return nil
		},
	}

	applied, err := runMigrations(context.Background(), db, 26)
	if err != nil {
		t.Fatalf("runMigrations should tolerate duplicate column in one statement while keeping the migration active: %v", err)
	}
	if applied != LatestVersion()-26 {
		t.Fatalf("applied = %d, want %d", applied, LatestVersion()-26)
	}
	if !sawIssuesStartedAt {
		t.Fatal("migration 0027 did not attempt issues.started_at ALTER")
	}
	if !sawWispsStartedAt {
		t.Fatal("migration 0027 did not attempt wisps.started_at ALTER")
	}
}
