//go:build js && wasm

package wasmsqlite

import (
	"database/sql"
	"fmt"
	"sync"
	"syscall/js"
	"time"
)

// BridgeAdapter adapts the JavaScript SQLite bridge to work with our Go driver
type BridgeAdapter struct {
	bridge js.Value
	dbID   string
	mu     sync.Mutex
}

// NewBridgeAdapter creates a new bridge adapter
func NewBridgeAdapter() (*BridgeAdapter, error) {
	bridge := js.Global().Get("sqliteBridge")
	if bridge.IsUndefined() {
		return nil, fmt.Errorf("sqliteBridge not found - ensure sqlite-bridge.js is loaded")
	}

	return &BridgeAdapter{
		bridge: bridge,
	}, nil
}

// Open opens a database
func (b *BridgeAdapter) Open(opts *Options) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	openMethod := b.bridge.Get("open")
	if openMethod.IsUndefined() {
		return "", fmt.Errorf("sqliteBridge.open method not found")
	}

	jsOpts := js.Global().Get("Object").New()
	jsOpts.Set("file", opts.File)
	jsOpts.Set("vfs", opts.VFS)
	jsOpts.Set("busyTimeout", opts.BusyTimeout)
	jsOpts.Set("mode", opts.Mode)
	jsOpts.Set("cache", opts.Cache)
	jsOpts.Set("journalMode", opts.JournalMode)
	jsOpts.Set("requirePersistent", opts.RequirePersistent)

	pragmas := js.Global().Get("Array").New(len(opts.Pragma))
	for i, pragma := range opts.Pragma {
		pragmas.SetIndex(i, pragma)
	}
	jsOpts.Set("pragma", pragmas)

	result, err := b.callAsync(openMethod, jsOpts)
	if err != nil {
		return "", err
	}

	vfsType := "unknown"
	if !result.IsUndefined() && !result.Get("vfsType").IsUndefined() {
		vfsType = result.Get("vfsType").String()
	}
	if !result.IsUndefined() && !result.Get("dbId").IsUndefined() {
		b.dbID = result.Get("dbId").String()
	}
	if b.dbID == "" {
		return "", fmt.Errorf("sqliteBridge.open did not return dbId")
	}

	return vfsType, nil
}

// Exec executes a SQL statement
func (b *BridgeAdapter) Exec(sql string, params []interface{}) (int, int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	execMethod := b.bridge.Get("exec")
	if execMethod.IsUndefined() {
		return 0, 0, fmt.Errorf("sqliteBridge.exec method not found")
	}

	// Convert params to JavaScript array
	jsParams := js.Global().Get("Array").New()
	for i, param := range params {
		jsParams.SetIndex(i, b.toJSValue(param))
	}

	result, err := b.callAsync(execMethod, b.dbID, sql, jsParams)
	if err != nil {
		return 0, 0, err
	}

	// Extract rowsAffected and lastInsertId
	rowsAffected := 0
	lastInsertId := 0

	if !result.IsUndefined() {
		if !result.Get("rowsAffected").IsUndefined() {
			rowsAffected = result.Get("rowsAffected").Int()
		}
		if !result.Get("lastInsertId").IsUndefined() {
			lastInsertId = result.Get("lastInsertId").Int()
		}
	}

	return rowsAffected, lastInsertId, nil
}

// Query executes a query and returns results
func (b *BridgeAdapter) Query(sql string, params []interface{}) ([]string, [][]interface{}, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	queryMethod := b.bridge.Get("query")
	if queryMethod.IsUndefined() {
		return nil, nil, fmt.Errorf("sqliteBridge.query method not found")
	}

	// Convert params to JavaScript array
	jsParams := js.Global().Get("Array").New()
	for i, param := range params {
		jsParams.SetIndex(i, b.toJSValue(param))
	}

	result, err := b.callAsync(queryMethod, b.dbID, sql, jsParams)
	if err != nil {
		return nil, nil, err
	}

	// Extract columns and rows
	var columns []string
	var rows [][]interface{}

	if !result.IsUndefined() {
		// Get columns
		columnsJS := result.Get("columns")
		if !columnsJS.IsUndefined() && columnsJS.Length() > 0 {
			columns = make([]string, columnsJS.Length())
			for i := 0; i < columnsJS.Length(); i++ {
				columns[i] = columnsJS.Index(i).String()
			}
		}

		// Get rows
		rowsJS := result.Get("rows")
		if !rowsJS.IsUndefined() && rowsJS.Length() > 0 {
			rows = make([][]interface{}, rowsJS.Length())
			for i := 0; i < rowsJS.Length(); i++ {
				rowJS := rowsJS.Index(i)
				if rowJS.Length() > 0 {
					row := make([]interface{}, rowJS.Length())
					for j := 0; j < rowJS.Length(); j++ {
						val := rowJS.Index(j)
						if val.IsNull() {
							row[j] = nil
						} else if val.Type() == js.TypeNumber {
							num := val.Float()
							// If it's a whole number, return as int64 to match SQLite integer types
							if num == float64(int64(num)) {
								row[j] = int64(num)
							} else {
								row[j] = num
							}
						} else if isUint8Array(val) {
							row[j] = uint8ArrayToBytes(val)
						} else if val.Type() == js.TypeString {
							row[j] = val.String()
						} else if val.Type() == js.TypeBoolean {
							row[j] = val.Bool()
						} else {
							row[j] = val.String()
						}
					}
					rows[i] = row
				}
			}
		}
	}

	return columns, rows, nil
}

// Begin starts a transaction
func (b *BridgeAdapter) Begin() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	beginMethod := b.bridge.Get("begin")
	if beginMethod.IsUndefined() {
		return fmt.Errorf("sqliteBridge.begin method not found")
	}

	_, err := b.callAsync(beginMethod, b.dbID)
	return err
}

// Commit commits a transaction
func (b *BridgeAdapter) Commit() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	commitMethod := b.bridge.Get("commit")
	if commitMethod.IsUndefined() {
		return fmt.Errorf("sqliteBridge.commit method not found")
	}

	_, err := b.callAsync(commitMethod, b.dbID)
	return err
}

// Rollback rolls back a transaction
func (b *BridgeAdapter) Rollback() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	rollbackMethod := b.bridge.Get("rollback")
	if rollbackMethod.IsUndefined() {
		return fmt.Errorf("sqliteBridge.rollback method not found")
	}

	_, err := b.callAsync(rollbackMethod, b.dbID)
	return err
}

// Close closes the database connection
func (b *BridgeAdapter) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	closeMethod := b.bridge.Get("close")
	if closeMethod.IsUndefined() {
		return fmt.Errorf("sqliteBridge.close method not found")
	}

	_, err := b.callAsync(closeMethod, b.dbID)
	if err == nil {
		b.dbID = ""
	}
	return err
}

// Dump exports the database as SQL statements
func (b *BridgeAdapter) Dump() (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	dumpMethod := b.bridge.Get("dump")
	if dumpMethod.IsUndefined() {
		return "", fmt.Errorf("sqliteBridge.dump method not found")
	}

	result, err := b.callAsync(dumpMethod, b.dbID)
	if err != nil {
		return "", err
	}

	// Extract dump from result
	if !result.IsUndefined() && !result.IsNull() {
		dump := result.Get("dump")
		if dump.Truthy() {
			return dump.String(), nil
		}
	}

	return "", fmt.Errorf("no dump data received")
}

// Load imports SQL statements to restore the database
func (b *BridgeAdapter) Load(dump string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	loadMethod := b.bridge.Get("load")
	if loadMethod.IsUndefined() {
		return fmt.Errorf("sqliteBridge.load method not found")
	}

	_, err := b.callAsync(loadMethod, b.dbID, dump)
	return err
}

func isUint8Array(val js.Value) bool {
	uint8Array := js.Global().Get("Uint8Array")
	return !uint8Array.IsUndefined() && val.InstanceOf(uint8Array)
}

func uint8ArrayToBytes(val js.Value) []byte {
	bytes := make([]byte, val.Get("byteLength").Int())
	js.CopyBytesToGo(bytes, val)
	return bytes
}

// callAsync calls a JavaScript async function and waits for the result
func (b *BridgeAdapter) callAsync(method js.Value, args ...interface{}) (js.Value, error) {
	// Call the method
	promise := method.Invoke(args...)
	if promise.IsUndefined() {
		return js.Undefined(), fmt.Errorf("method did not return a promise")
	}

	// Wait for the promise to resolve
	done := make(chan struct {
		result js.Value
		err    error
	}, 1)

	// Handle promise resolution
	then := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer func() {
			if r := recover(); r != nil {
				done <- struct {
					result js.Value
					err    error
				}{js.Undefined(), fmt.Errorf("promise then handler panicked: %v", r)}
			}
		}()

		var result js.Value
		if len(args) > 0 {
			result = args[0]
		} else {
			result = js.Undefined()
		}

		// Check if result indicates error
		if !result.IsUndefined() && !result.Get("ok").IsUndefined() {
			if !result.Get("ok").Bool() {
				errorMsg := "unknown error"
				if !result.Get("error").IsUndefined() {
					errorMsg = result.Get("error").String()
				}
				done <- struct {
					result js.Value
					err    error
				}{js.Undefined(), fmt.Errorf("%s", errorMsg)}
				return nil
			}
		}

		done <- struct {
			result js.Value
			err    error
		}{result, nil}
		return nil
	})
	defer then.Release()

	// Handle promise rejection
	catch := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer func() {
			if r := recover(); r != nil {
				done <- struct {
					result js.Value
					err    error
				}{js.Undefined(), fmt.Errorf("promise catch handler panicked: %v", r)}
			}
		}()

		errorMsg := "unknown error"
		if len(args) > 0 {
			error := args[0]
			// Try to extract more details from the error
			if !error.IsUndefined() {
				if !error.Get("message").IsUndefined() {
					errorMsg = error.Get("message").String()
				} else if !error.Get("toString").IsUndefined() {
					errorMsg = error.Call("toString").String()
				} else {
					errorMsg = error.String()
				}
			}
			fmt.Printf("JavaScript error details: %s\n", errorMsg)
		}

		done <- struct {
			result js.Value
			err    error
		}{js.Undefined(), fmt.Errorf("%s", errorMsg)}
		return nil
	})
	defer catch.Release()

	// Attach handlers
	promise.Call("then", then).Call("catch", catch)

	// Wait for completion
	result := <-done
	return result.result, result.err
}

// toJSValue safely converts a Go value to a JavaScript value, handling nil and special cases
func (b *BridgeAdapter) toJSValue(v interface{}) js.Value {
	if v == nil {
		return js.Null()
	}

	// Handle common database types
	switch val := v.(type) {
	case nil:
		return js.Null()
	case bool, int, int8, int16, int32, uint, uint8, uint16, uint32, float32, float64, string:
		// These types are handled directly by js.ValueOf
		return js.ValueOf(val)
	case int64:
		return int64ToJSValue(val)
	case uint64:
		if val <= uint64(1<<63-1) {
			return int64ToJSValue(int64(val))
		}
		return js.ValueOf(fmt.Sprintf("%d", val))
	case []byte:
		// Convert byte slice to Uint8Array
		if val == nil {
			return js.Null()
		}
		uint8Array := js.Global().Get("Uint8Array").New(len(val))
		if len(val) > 0 {
			js.CopyBytesToJS(uint8Array, val)
		}
		return uint8Array
	case time.Time:
		// Convert time to ISO string
		if val.IsZero() {
			return js.Null()
		}
		return js.ValueOf(val.Format(time.RFC3339Nano))
	case *time.Time:
		// Handle pointer to time
		if val == nil || val.IsZero() {
			return js.Null()
		}
		return js.ValueOf(val.Format(time.RFC3339Nano))
	case sql.NullString:
		if val.Valid {
			return js.ValueOf(val.String)
		}
		return js.Null()
	case sql.NullBool:
		if val.Valid {
			return js.ValueOf(val.Bool)
		}
		return js.Null()
	case sql.NullInt64:
		if val.Valid {
			return int64ToJSValue(val.Int64)
		}
		return js.Null()
	case sql.NullFloat64:
		if val.Valid {
			return js.ValueOf(val.Float64)
		}
		return js.Null()
	case sql.NullTime:
		if val.Valid {
			return js.ValueOf(val.Time.Format(time.RFC3339Nano))
		}
		return js.Null()
	default:
		// For any other type, try to convert it to a string
		// This prevents panics but may not be ideal for all types
		if val == nil {
			return js.Null()
		}
		// Use fmt.Sprint as a fallback
		return js.ValueOf(fmt.Sprintf("%v", val))
	}
}

func int64ToJSValue(val int64) js.Value {
	const maxSafeInteger = int64(1<<53 - 1)
	const minSafeInteger = -maxSafeInteger

	if val >= minSafeInteger && val <= maxSafeInteger {
		return js.ValueOf(val)
	}

	obj := js.Global().Get("Object").New()
	obj.Set("__wasmSqliteInt64", fmt.Sprintf("%d", val))
	return obj
}
