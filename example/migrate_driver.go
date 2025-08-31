//go:build js && wasm

package main

import (
	"database/sql"
	"fmt"
	"io"
	"strings"

	"github.com/golang-migrate/migrate/v4/database"
	"github.com/hashicorp/go-multierror"
)

// WASMSQLiteDriver implements the database.Driver interface for golang-migrate
type WASMSQLiteDriver struct {
	db *sql.DB
}

// NewWASMSQLiteDriver creates a new migrate driver for WASM SQLite
func NewWASMSQLiteDriver(db *sql.DB) (database.Driver, error) {
	if db == nil {
		return nil, fmt.Errorf("db cannot be nil")
	}
	
	driver := &WASMSQLiteDriver{
		db: db,
	}
	
	// Ensure migration table exists
	if err := driver.ensureVersionTable(); err != nil {
		return nil, err
	}
	
	return driver, nil
}

func (d *WASMSQLiteDriver) ensureVersionTable() error {
	query := `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		dirty BOOLEAN NOT NULL DEFAULT FALSE
	)`
	_, err := d.db.Exec(query)
	return err
}

// Open returns the underlying database connection
func (d *WASMSQLiteDriver) Open(url string) (database.Driver, error) {
	return nil, fmt.Errorf("Open is not supported for WASMSQLiteDriver, use NewWASMSQLiteDriver instead")
}

// Close closes the underlying database connection
func (d *WASMSQLiteDriver) Close() error {
	// We don't close the connection here as it's managed externally
	return nil
}

// Lock acquires a lock (no-op for SQLite in WASM)
func (d *WASMSQLiteDriver) Lock() error {
	// SQLite has built-in locking, no need for additional locking in WASM context
	return nil
}

// Unlock releases the lock (no-op for SQLite in WASM)
func (d *WASMSQLiteDriver) Unlock() error {
	return nil
}

// Run executes a migration
func (d *WASMSQLiteDriver) Run(migration io.Reader) error {
	migrationBytes, err := io.ReadAll(migration)
	if err != nil {
		return err
	}
	
	query := string(migrationBytes)
	
	// Split by semicolons but be careful with strings
	statements := splitSQLStatements(query)
	
	// Execute each statement in a transaction
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()
	
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err = tx.Exec(stmt); err != nil {
			return fmt.Errorf("failed to execute statement: %v\nStatement: %s", err, stmt)
		}
	}
	
	return tx.Commit()
}

// SetVersion sets the current migration version
func (d *WASMSQLiteDriver) SetVersion(version int, dirty bool) error {
	query := `DELETE FROM schema_migrations`
	if _, err := d.db.Exec(query); err != nil {
		return err
	}
	
	if version >= 0 {
		query = `INSERT INTO schema_migrations (version, dirty) VALUES (?, ?)`
		if _, err := d.db.Exec(query, version, dirty); err != nil {
			return err
		}
	}
	
	return nil
}

// Version returns the current migration version
func (d *WASMSQLiteDriver) Version() (version int, dirty bool, err error) {
	query := `SELECT version, dirty FROM schema_migrations LIMIT 1`
	
	row := d.db.QueryRow(query)
	err = row.Scan(&version, &dirty)
	if err == sql.ErrNoRows {
		return database.NilVersion, false, nil
	}
	
	return version, dirty, err
}

// Drop drops all tables
func (d *WASMSQLiteDriver) Drop() error {
	// Get all table names
	query := `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`
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
	var result error
	for _, table := range tables {
		query := fmt.Sprintf("DROP TABLE IF EXISTS %s", table)
		if _, err := d.db.Exec(query); err != nil {
			result = multierror.Append(result, err)
		}
	}
	
	return result
}

// splitSQLStatements splits SQL statements by semicolon, respecting quoted strings
func splitSQLStatements(sql string) []string {
	var statements []string
	var current strings.Builder
	inString := false
	var stringChar rune
	
	runes := []rune(sql)
	for i := 0; i < len(runes); i++ {
		char := runes[i]
		
		// Handle string literals
		if !inString && (char == '\'' || char == '"') {
			inString = true
			stringChar = char
			current.WriteRune(char)
		} else if inString && char == stringChar {
			// Check if it's escaped
			if i+1 < len(runes) && runes[i+1] == stringChar {
				current.WriteRune(char)
				current.WriteRune(runes[i+1])
				i++ // Skip next character
			} else {
				inString = false
				current.WriteRune(char)
			}
		} else if !inString && char == ';' {
			// End of statement
			if stmt := strings.TrimSpace(current.String()); stmt != "" {
				statements = append(statements, stmt)
			}
			current.Reset()
		} else {
			current.WriteRune(char)
		}
	}
	
	// Add any remaining statement
	if stmt := strings.TrimSpace(current.String()); stmt != "" {
		statements = append(statements, stmt)
	}
	
	return statements
}