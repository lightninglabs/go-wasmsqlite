//go:build js && wasm

package wasmsqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"time"
)

// VFSType represents the type of virtual file system used by the database
type VFSType string

const (
	VFSTypeAuto         VFSType = "auto"
	VFSTypeOPFS         VFSType = "opfs"
	VFSTypeOPFSWebLocks VFSType = "opfs-wl"
	VFSTypeOPFSSAHPool  VFSType = "opfs-sahpool"
	VFSTypeMemory       VFSType = "memory"
	VFSTypeUnknown      VFSType = "unknown"
)

// StorageInfo reports the database storage backend selected by the browser.
type StorageInfo struct {
	// RequestedVFS is the VFS requested by Options or the DSN.
	RequestedVFS VFSType

	// VFSType is the VFS actually used by SQLite.
	VFSType VFSType

	// Persistent is true when the database is backed by persistent browser
	// storage instead of transient memory.
	Persistent bool

	// Memory is true when the database is using SQLite's in-memory storage.
	Memory bool

	// OPFS is true when the resolved VFS is one of SQLite's OPFS-backed VFSes.
	OPFS bool
}

// GetVFSType returns the type of VFS being used by the connection
func GetVFSType(conn *sql.Conn) (VFSType, error) {
	var vfsType VFSType

	err := conn.Raw(func(driverConn interface{}) error {
		c, ok := driverConn.(*Conn)
		if !ok {
			return fmt.Errorf("not a wasmsqlite connection")
		}

		// The VFS type is stored when the database is opened
		// We'll expose it through a method
		vfsType = c.GetVFSType()
		return nil
	})

	if err != nil {
		return VFSTypeUnknown, err
	}

	return vfsType, nil
}

// GetStorageInfo returns the storage backend selected by the browser for db.
func GetStorageInfo(db *sql.DB) (StorageInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return GetStorageInfoContext(ctx, db)
}

// GetStorageInfoContext returns the storage backend selected by the browser for
// db while respecting the provided context.
func GetStorageInfoContext(ctx context.Context, db *sql.DB) (StorageInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return StorageInfo{VFSType: VFSTypeUnknown}, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	var info StorageInfo
	err = conn.Raw(func(driverConn interface{}) error {
		c, ok := driverConn.(*Conn)
		if !ok {
			return fmt.Errorf("not a wasmsqlite connection")
		}

		info = c.StorageInfo()
		return nil
	})
	if err != nil {
		return StorageInfo{VFSType: VFSTypeUnknown}, err
	}

	return info, nil
}

// ExecBatch executes query once for each positional argument row in one worker
// request. The SQLite worker wraps the batch in a single transaction and
// prepares the statement once for the whole batch.
func ExecBatch(db *sql.DB, query string, rows [][]any) (sql.Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return ExecBatchContext(ctx, db, query, rows)
}

// ExecBatchContext executes query once for each positional argument row in one
// worker request while respecting the provided context. Context cancellation
// stops waiting on the Go side; it does not interrupt a batch already posted to
// the SQLite worker.
func ExecBatchContext(ctx context.Context, db *sql.DB, query string, rows [][]any) (sql.Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	batchRows := make([][]driver.NamedValue, len(rows))
	for i, row := range rows {
		batchRows[i] = make([]driver.NamedValue, len(row))
		for j, value := range row {
			batchRows[i][j] = driver.NamedValue{
				Ordinal: j + 1,
				Value:   value,
			}
		}
	}

	var result sql.Result
	err = conn.Raw(func(driverConn interface{}) error {
		c, ok := driverConn.(*Conn)
		if !ok {
			return fmt.Errorf("not a wasmsqlite connection")
		}

		res, err := c.ExecBatchContext(ctx, query, batchRows)
		if err != nil {
			return err
		}
		result = res
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to execute batch: %w", err)
	}

	return result, nil
}

// DumpDatabase exports the entire database as SQL statements
func DumpDatabase(db *sql.DB) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return DumpDatabaseContext(ctx, db)
}

// DumpDatabaseContext exports the entire database as SQL statements while
// respecting the provided context.
func DumpDatabaseContext(ctx context.Context, db *sql.DB) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	var dump string
	err = conn.Raw(func(driverConn interface{}) error {
		c, ok := driverConn.(*Conn)
		if !ok {
			return fmt.Errorf("not a wasmsqlite connection")
		}

		// Send dump request to Worker
		dumpStr, err := c.Dump(ctx)
		if err != nil {
			return err
		}
		dump = dumpStr
		return nil
	})

	if err != nil {
		return "", fmt.Errorf("failed to dump database: %w", err)
	}

	return dump, nil
}

// LoadDatabase imports SQL statements to restore a database
func LoadDatabase(db *sql.DB, dump string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return LoadDatabaseContext(ctx, db, dump)
}

// LoadDatabaseContext imports SQL statements to restore a database while
// respecting the provided context.
func LoadDatabaseContext(ctx context.Context, db *sql.DB, dump string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	err = conn.Raw(func(driverConn interface{}) error {
		c, ok := driverConn.(*Conn)
		if !ok {
			return fmt.Errorf("not a wasmsqlite connection")
		}

		// Send load request to Worker
		return c.Load(ctx, dump)
	})

	if err != nil {
		return fmt.Errorf("failed to load database: %w", err)
	}

	return nil
}
