package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenMigratesAndPreservesData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guardian data #1.sqlite")
	db := openTestDatabase(t, path)
	var version int
	if err := db.QueryRow("SELECT version FROM configuration WHERE id = 1").Scan(&version); err != nil || version != 1 {
		t.Fatalf("initial configuration version = %d, error = %v", version, err)
	}
	if _, err := db.Exec("UPDATE configuration SET version = 2 WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db = openTestDatabase(t, path)
	if err := db.QueryRow("SELECT version FROM configuration WHERE id = 1").Scan(&version); err != nil || version != 2 {
		t.Fatalf("persisted configuration version = %d, error = %v", version, err)
	}
}

func TestChannelAndRequestConstraints(t *testing.T) {
	db := openTestDatabase(t, filepath.Join(t.TempDir(), "guardian.sqlite"))
	if _, err := db.Exec("INSERT INTO channels VALUES ('channel', 'Name', 'unknown', 1000)"); err == nil {
		t.Fatal("stored an unknown channel as an explicit decision")
	}

	insertPending := "INSERT INTO approval_requests (channel_id, channel_name, video_url, requested_at_ms) VALUES ('channel', 'Name', 'https://www.youtube.com/watch?v=video', 1000)"
	if _, err := db.Exec(insertPending); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(insertPending); err == nil {
		t.Fatal("created a second pending request for the same channel")
	}
	if _, err := db.Exec("UPDATE approval_requests SET status = 'rejected'"); err == nil {
		t.Fatal("resolved a request without a resolution time")
	}
	if _, err := db.Exec("UPDATE approval_requests SET status = 'rejected', resolved_at_ms = 2000"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(insertPending); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationRollsBackOnFailure(t *testing.T) {
	db := openTestDatabase(t, filepath.Join(t.TempDir(), "guardian.sqlite"))
	// Leave a conflicting table so the first migration fails after creating channels.
	if _, err := db.Exec("DROP TABLE channels; PRAGMA user_version = 0"); err != nil {
		t.Fatal(err)
	}
	if err := applyMigrations(context.Background(), db); err == nil {
		t.Fatal("conflicting schema migration succeeded")
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name = 'channels'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partially migrated table count = %d, error = %v", count, err)
	}
}

func TestRejectsNewerSchema(t *testing.T) {
	db := openTestDatabase(t, filepath.Join(t.TempDir(), "guardian.sqlite"))
	if _, err := db.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	err := applyMigrations(context.Background(), db)
	if err == nil || !strings.Contains(err.Error(), "unsupported schema version 999") {
		t.Fatalf("newer schema error = %v", err)
	}
}

func openTestDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
