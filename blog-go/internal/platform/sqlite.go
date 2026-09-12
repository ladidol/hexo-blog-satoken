package platform

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// 用途：打开唯一同机数据库并配置串行写与持久一致性。
func OpenSQLite(cfg Config) (*sql.DB, error) {
	if cfg.DataDir == "" {
		return nil, errors.New("data directory is required")
	}
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	databasePath, err := filepath.Abs(filepath.Join(cfg.DataDir, "blog.db"))
	if err != nil {
		return nil, err
	}
	dsn := url.URL{Scheme: "file", Path: databasePath}
	query := url.Values{
		"_foreign_keys": {"on"}, "_journal_mode": {"WAL"},
		"_busy_timeout": {"5000"}, "_txlock": {"immediate"},
	}
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("sqlite3", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect SQLite: %w", err)
	}
	return db, nil
}

// 用途：按 user_version 将本评审完整声明首次装入数据库。
func Migrate(ctx context.Context, db *sql.DB, schemas []string, seed func(context.Context, *sql.Tx) error) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	created := &MigrationFiles{}
	committed := false
	defer func() {
		_ = tx.Rollback()
		if !committed {
			for _, path := range created.Paths {
				if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					err = errors.Join(err, fmt.Errorf("remove uncommitted seed file: %w", removeErr))
				}
			}
		}
	}()
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version == 1 {
		return tx.Commit()
	}
	if version != 0 {
		return fmt.Errorf("unsupported schema version %d", version)
	}
	if len(schemas) == 0 {
		return errors.New("migration schemas are required")
	}
	for index, schema := range schemas {
		if _, err := tx.ExecContext(ctx, schema); err != nil {
			return fmt.Errorf("apply schema %d: %w", index, err)
		}
	}
	if seed != nil {
		seedContext := context.WithValue(ctx, (*MigrationFiles)(nil), created)
		if err := seed(seedContext, tx); err != nil {
			return fmt.Errorf("seed database: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, "PRAGMA user_version=1"); err != nil {
		return fmt.Errorf("set schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	committed = true
	return nil
}

// MigrationFiles is transaction-local compensation metadata, not a persistent
// migration table. Seed records only files it exclusively created. The pointer
// type's nil value is the context key, keeping the frozen Migrate/Seed signatures.
type MigrationFiles struct {
	Paths []string
}
