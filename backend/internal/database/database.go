package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/snirye/youtube-guardian/backend/migrations"
)

func Open(ctx context.Context, path string) (*sql.DB, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	dsn := url.URL{Scheme: "file", Path: path}
	options := url.Values{
		"_pragma": {"busy_timeout(5000)", "foreign_keys(1)"},
		"_txlock": {"immediate"},
	}
	dsn.RawQuery = options.Encode()

	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// Serialize access to this process's small local database.
	db.SetMaxOpenConns(1)
	if err := applyMigrations(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	return db, nil
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	scripts := migrations.Ordered()
	if version < 0 || version > len(scripts) {
		return fmt.Errorf("unsupported schema version %d (latest supported: %d)", version, len(scripts))
	}
	for i, script := range scripts[version:] {
		nextVersion := version + i + 1
		if _, err := tx.ExecContext(ctx, script); err != nil {
			return fmt.Errorf("apply migration %d: %w", nextVersion, err)
		}
		if _, err := tx.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(nextVersion)); err != nil {
			return fmt.Errorf("record migration %d: %w", nextVersion, err)
		}
	}
	return tx.Commit()
}
