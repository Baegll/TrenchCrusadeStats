// Package db manages the DuckDB connection, schema migrations, and typed query functions.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	_ "github.com/marcboeker/go-duckdb"
)

// DB wraps the DuckDB connection pool and write coordination.
type DB struct {
	pool    *sql.DB
	writeMu sync.Mutex
}

// Migrations holds embedded migration SQL keyed by version number.
type Migrations map[int]string

// Open creates a new DuckDB connection pool and runs pending migrations.
// If the WAL file is corrupted, it attempts recovery by removing the WAL.
func Open(ctx context.Context, path string, migrations Migrations) (*DB, error) {
	pool, err := sql.Open("duckdb", path)
	if err != nil {
		if strings.Contains(err.Error(), "Failure while replaying WAL") {
			slog.Error("WAL corruption detected, attempting recovery", "path", path, "err", err)
			walPath := path + ".wal"
			if rmErr := os.Remove(walPath); rmErr != nil {
				return nil, fmt.Errorf("failed to remove corrupted WAL %s: %w (original: %v)", walPath, rmErr, err)
			}
			slog.Info("removed corrupted WAL file, retrying open", "wal", walPath)
			pool, err = sql.Open("duckdb", path)
		}
		if err != nil {
			return nil, fmt.Errorf("opening duckdb at %s: %w", path, err)
		}
	}

	pool.SetMaxOpenConns(4)
	pool.SetMaxIdleConns(2)

	if err := pool.PingContext(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging duckdb: %w", err)
	}

	d := &DB{pool: pool}

	// Load DuckDB JSON extension (required for JSON column type)
	if _, err := pool.ExecContext(ctx, "INSTALL json; LOAD json;"); err != nil {
		// May already be loaded or auto-loaded; try just LOAD
		if _, err2 := pool.ExecContext(ctx, "LOAD json;"); err2 != nil {
			pool.Close()
			return nil, fmt.Errorf("loading json extension: %w (install err: %w)", err2, err)
		}
	}

	if err := d.ensureSchemaVersion(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ensuring schema_version table: %w", err)
	}

	if err := d.migrate(ctx, migrations); err != nil {
		pool.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	if err := d.seedFactions(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("seeding factions: %w", err)
	}

	slog.Info("duckdb ready", "path", path)
	return d, nil
}

// Close shuts down the connection pool.
func (d *DB) Close() error {
	return d.pool.Close()
}

// Pool returns the underlying sql.DB for read queries.
func (d *DB) Pool() *sql.DB {
	return d.pool
}

// AcquireWriter locks the write mutex and returns a dedicated connection.
// The caller MUST call the returned release function when done.
func (d *DB) AcquireWriter(ctx context.Context) (*sql.Conn, func(), error) {
	d.writeMu.Lock()
	conn, err := d.pool.Conn(ctx)
	if err != nil {
		d.writeMu.Unlock()
		return nil, nil, fmt.Errorf("acquiring write connection: %w", err)
	}
	release := func() {
		conn.Close()
		d.writeMu.Unlock()
	}
	return conn, release, nil
}

// TryAcquireWriter attempts to lock the write mutex without blocking.
// Returns locked=false if a write is already in progress.
// Returns locked=false with a non-nil error if the mutex was acquired but the connection failed.
func (d *DB) TryAcquireWriter(ctx context.Context) (conn *sql.Conn, release func(), locked bool, err error) {
	if !d.writeMu.TryLock() {
		return nil, nil, false, nil
	}
	conn, err = d.pool.Conn(ctx)
	if err != nil {
		d.writeMu.Unlock()
		return nil, nil, false, fmt.Errorf("acquiring write connection: %w", err)
	}
	release = func() {
		conn.Close()
		d.writeMu.Unlock()
	}
	return conn, release, true, nil
}

// ensureSchemaVersion creates the schema_version table if it doesn't exist.
func (d *DB) ensureSchemaVersion(ctx context.Context) error {
	_, err := d.pool.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_version (
			version     INTEGER PRIMARY KEY,
			applied_at  TIMESTAMP DEFAULT current_timestamp,
			description VARCHAR NOT NULL
		)
	`)
	return err
}

// migrate runs any unapplied migrations in order.
// The runner records the version AFTER the DDL succeeds — migration SQL must NOT insert into schema_version.
func (d *DB) migrate(ctx context.Context, migrations Migrations) error {
	var current int
	if err := d.pool.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&current); err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}
	slog.Info("schema version", "current", current)

	for v := current + 1; ; v++ {
		ddl, ok := migrations[v]
		if !ok {
			break
		}
		if _, err := d.pool.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("applying migration %03d: %w", v, err)
		}
		if _, err := d.pool.ExecContext(ctx,
			"INSERT INTO schema_version (version, description) VALUES ($1, $2)",
			v, fmt.Sprintf("migration_%03d", v),
		); err != nil {
			return fmt.Errorf("recording migration %03d: %w", v, err)
		}
		slog.Info("applied migration", "version", v)
	}
	return nil
}

// ExecTx runs fn inside a transaction on the given connection.
func ExecTx(ctx context.Context, conn *sql.Conn, fn func(tx *sql.Tx) error) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("rollback failed: %w (original: %w)", rbErr, err)
		}
		return err
	}
	return tx.Commit()
}

// BackgroundCtx returns a context with a timeout, independent of any parent.
// Used for cleanup operations that must complete even if the caller's context is cancelled.
func BackgroundCtx(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}
