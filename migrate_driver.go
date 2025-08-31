//go:build js && wasm

package wasmsqlite

import (
	"database/sql"
	"fmt"
	"io"

	"github.com/golang-migrate/migrate/v4/database"
	"github.com/hashicorp/go-multierror"
)

// MigrateDriver implements the database.Driver interface for golang-migrate.
// It provides migration support for SQLite databases running in WebAssembly.
//
// This driver is based on the official golang-migrate sqlite3 driver but adapted
// for WASM constraints:
// - No file-based operations (uses existing *sql.DB connection)
// - No locking needed (WASM is single-threaded)
// - Simplified configuration (no x-migrations-table or x-no-tx-wrap options)
// - Always uses transactions for safety
type MigrateDriver struct {
	db *sql.DB
}

// NewMigrateDriver creates a new migrate driver for WASM SQLite.
// The returned driver can be used with golang-migrate to manage database migrations.
func NewMigrateDriver(db *sql.DB) (database.Driver, error) {
	if db == nil {
		return nil, fmt.Errorf("db cannot be nil")
	}
	
	driver := &MigrateDriver{
		db: db,
	}
	
	// Ensure migration table exists
	if err := driver.ensureVersionTable(); err != nil {
		return nil, err
	}
	
	return driver, nil
}

func (d *MigrateDriver) ensureVersionTable() error {
	// Create both table and unique index like the original driver
	query := `
	CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER, dirty BOOLEAN);
	CREATE UNIQUE INDEX IF NOT EXISTS version_unique ON schema_migrations (version);
	`
	_, err := d.db.Exec(query)
	return err
}

// Open returns the underlying database connection.
// This method is not supported for MigrateDriver - use NewMigrateDriver instead.
func (d *MigrateDriver) Open(url string) (database.Driver, error) {
	return nil, fmt.Errorf("Open is not supported for MigrateDriver, use NewMigrateDriver instead")
}

// Close closes the underlying database connection
func (d *MigrateDriver) Close() error {
	// We don't close the connection here as it's managed externally
	return nil
}

// Lock acquires a lock (no-op for SQLite in WASM)
func (d *MigrateDriver) Lock() error {
	// SQLite has built-in locking, no need for additional locking in WASM context
	return nil
}

// Unlock releases the lock (no-op for SQLite in WASM)
func (d *MigrateDriver) Unlock() error {
	return nil
}

// Run executes a migration
func (d *MigrateDriver) Run(migration io.Reader) error {
	migrationBytes, err := io.ReadAll(migration)
	if err != nil {
		return err
	}
	
	query := string(migrationBytes)
	
	// Execute migration in a transaction
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("transaction start failed: %w", err)
	}
	
	// SQLite can handle multiple statements in a single Exec call
	// when they're separated by semicolons, similar to the original driver
	if _, err := tx.Exec(query); err != nil {
		if errRollback := tx.Rollback(); errRollback != nil {
			err = multierror.Append(err, errRollback)
		}
		return fmt.Errorf("migration failed: %w", err)
	}
	
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("transaction commit failed: %w", err)
	}
	
	return nil
}

// SetVersion sets the current migration version
func (d *MigrateDriver) SetVersion(version int, dirty bool) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("transaction start failed: %w", err)
	}
	
	query := `DELETE FROM schema_migrations`
	if _, err := tx.Exec(query); err != nil {
		tx.Rollback()
		return err
	}
	
	// Also re-write the schema version for nil dirty versions to prevent
	// empty schema version for failed down migration on the first migration
	// See: https://github.com/golang-migrate/migrate/issues/330
	if version >= 0 || (version == database.NilVersion && dirty) {
		query = `INSERT INTO schema_migrations (version, dirty) VALUES (?, ?)`
		if _, err := tx.Exec(query, version, dirty); err != nil {
			if errRollback := tx.Rollback(); errRollback != nil {
				err = multierror.Append(err, errRollback)
			}
			return err
		}
	}
	
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("transaction commit failed: %w", err)
	}
	
	return nil
}

// Version returns the current migration version
func (d *MigrateDriver) Version() (version int, dirty bool, err error) {
	query := `SELECT version, dirty FROM schema_migrations LIMIT 1`
	
	row := d.db.QueryRow(query)
	err = row.Scan(&version, &dirty)
	if err == sql.ErrNoRows {
		return database.NilVersion, false, nil
	}
	
	return version, dirty, err
}

// Drop drops all tables
func (d *MigrateDriver) Drop() error {
	// Get all table names
	query := `SELECT name FROM sqlite_master WHERE type = 'table'`
	rows, err := d.db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return err
		}
		tables = append(tables, table)
	}
	
	if err := rows.Err(); err != nil {
		return err
	}
	
	// Drop all tables
	if len(tables) > 0 {
		for _, table := range tables {
			query := "DROP TABLE " + table
			if _, err := d.db.Exec(query); err != nil {
				return err
			}
		}
		// Vacuum to reclaim space, like the original driver
		if _, err := d.db.Exec("VACUUM"); err != nil {
			return err
		}
	}
	
	return nil
}