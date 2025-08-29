//go:build js && wasm

package wasmsqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// VFSType represents the type of virtual file system used by the database
type VFSType string

const (
	VFSTypeOPFS    VFSType = "opfs"
	VFSTypeMemory  VFSType = "memory"
	VFSTypeUnknown VFSType = "unknown"
)

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

// DumpDatabase exports the entire database as SQL statements
func DumpDatabase(db *sql.DB) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
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