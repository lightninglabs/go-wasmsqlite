//go:build js && wasm

package wasmsqlite

import (
	"database/sql/driver"
	"fmt"
	"io"
	"reflect"
	"time"
)

// Rows implements the database/sql/driver.Rows interface
type Rows struct {
	columns []string
	rows    [][]interface{}
	pos     int
}

// Columns implements driver.Rows
func (r *Rows) Columns() []string {
	return r.columns
}

// Close implements driver.Rows
func (r *Rows) Close() error {
	// Nothing to clean up for in-memory rows
	return nil
}

// Next implements driver.Rows
func (r *Rows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows) {
		return io.EOF
	}

	row := r.rows[r.pos]

	// Check if we have a valid row
	if row == nil {
		r.pos++
		return io.EOF
	}

	// Ensure dest has the right size
	if len(dest) != len(r.columns) {
		return fmt.Errorf("expected %d destination values, got %d", len(r.columns), len(dest))
	}

	// Copy values from row to dest
	for i := 0; i < len(dest) && i < len(row); i++ {
		dest[i] = convertInterfaceToDriverValue(row[i])
	}

	// Fill remaining dest slots with nil if row is shorter
	for i := len(row); i < len(dest); i++ {
		dest[i] = nil
	}

	r.pos++

	return nil
}

// ColumnTypeScanType implements driver.RowsColumnTypeScanType
func (r *Rows) ColumnTypeScanType(index int) reflect.Type {
	if index < 0 || index >= len(r.columns) {
		return nil
	}

	// Try to infer type from the first non-null value in this column
	for _, row := range r.rows {
		if index < len(row) && row[index] != nil {
			switch row[index].(type) {
			case string:
				return reflect.TypeOf("")
			case int64:
				return reflect.TypeOf(int64(0))
			case float64:
				return reflect.TypeOf(float64(0))
			case bool:
				return reflect.TypeOf(false)
			case []byte:
				return reflect.TypeOf([]byte{})
			case time.Time:
				return reflect.TypeOf(time.Time{})
			}
		}
	}

	// Default to interface{} if we can't determine the type
	return reflect.TypeOf((*interface{})(nil)).Elem()
}

// ColumnTypeDatabaseTypeName implements driver.RowsColumnTypeDatabaseTypeName
func (r *Rows) ColumnTypeDatabaseTypeName(index int) string {
	if index < 0 || index >= len(r.columns) {
		return ""
	}

	// Try to infer SQLite type from the first non-null value in this column
	for _, row := range r.rows {
		if index < len(row) && row[index] != nil {
			switch row[index].(type) {
			case string:
				return "TEXT"
			case int64:
				return "INTEGER"
			case float64:
				return "REAL"
			case bool:
				return "BOOLEAN"
			case []byte:
				return "BLOB"
			case time.Time:
				return "DATETIME"
			}
		}
	}

	return "NULL"
}

// ColumnTypeLength implements driver.RowsColumnTypeLength
func (r *Rows) ColumnTypeLength(index int) (length int64, ok bool) {
	// SQLite doesn't have fixed column lengths, so return false
	return 0, false
}

// ColumnTypeNullable implements driver.RowsColumnTypeNullable
func (r *Rows) ColumnTypeNullable(index int) (nullable, ok bool) {
	if index < 0 || index >= len(r.columns) {
		return false, false
	}

	// Check if any row has a null value in this column
	for _, row := range r.rows {
		if index < len(row) && row[index] == nil {
			return true, true
		}
	}

	// If we have data but no nulls, it's not nullable (at least not in this result set)
	if len(r.rows) > 0 {
		return false, true
	}

	// If we have no data, we can't determine nullability
	return false, false
}

// ColumnTypePrecisionScale implements driver.RowsColumnTypePrecisionScale
func (r *Rows) ColumnTypePrecisionScale(index int) (precision, scale int64, ok bool) {
	// SQLite doesn't have fixed precision/scale, so return false
	return 0, 0, false
}

// convertInterfaceToDriverValue converts an interface{} value to a driver.Value
func convertInterfaceToDriverValue(value interface{}) driver.Value {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case string:
		// Check if this looks like a timestamp and try to parse it
		// SQLite returns timestamps as strings in the format "YYYY-MM-DD HH:MM:SS"
		if len(v) == 19 && v[4] == '-' && v[7] == '-' && v[10] == ' ' && v[13] == ':' && v[16] == ':' {
			if t, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
				return t
			}
		}
		return v
	case int64:
		return v
	case float64:
		return v
	case bool:
		return v
	case []byte:
		return v
	case time.Time:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case float32:
		return float64(v)
	default:
		// Convert unknown types to string
		return fmt.Sprintf("%v", v)
	}
}
