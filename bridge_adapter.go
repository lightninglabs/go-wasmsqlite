//go:build js && wasm

package wasmsqlite

import (
	"fmt"
	"sync"
	"syscall/js"
)

// BridgeAdapter adapts the JavaScript SQLite bridge to work with our Go driver
type BridgeAdapter struct {
	bridge js.Value
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

// Init initializes the SQLite bridge
func (b *BridgeAdapter) Init() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	initMethod := b.bridge.Get("init")
	if initMethod.IsUndefined() {
		return fmt.Errorf("sqliteBridge.init method not found")
	}

	// The bridge auto-initializes on load, so we just return success
	return nil
}

// Open opens a database
func (b *BridgeAdapter) Open(filename, vfs string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	fmt.Printf("🔍 Bridge adapter opening database: filename=%s, vfs=%s\n", filename, vfs)

	openMethod := b.bridge.Get("open")
	if openMethod.IsUndefined() {
		return "", fmt.Errorf("sqliteBridge.open method not found")
	}

	fmt.Println("🔍 Found sqliteBridge.open method, calling it...")

	// Call the open method
	result, err := b.callAsync(openMethod, filename, vfs)
	if err != nil {
		fmt.Printf("❌ Bridge open failed: %v\n", err)
		return "", err
	}

	fmt.Printf("🔍 Bridge open result: %v\n", result)

	// Extract VFS type from result
	vfsType := "unknown"
	if !result.IsUndefined() && !result.Get("vfsType").IsUndefined() {
		vfsType = result.Get("vfsType").String()
		fmt.Printf("✅ VFS type extracted: %s\n", vfsType)
	} else {
		fmt.Printf("⚠️ vfsType not found in result\n")
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
		jsParams.SetIndex(i, param)
	}

	result, err := b.callAsync(execMethod, sql, jsParams)
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
		jsParams.SetIndex(i, param)
	}

	result, err := b.callAsync(queryMethod, sql, jsParams)
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

	_, err := b.callAsync(beginMethod)
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

	_, err := b.callAsync(commitMethod)
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

	_, err := b.callAsync(rollbackMethod)
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

	_, err := b.callAsync(closeMethod)
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

	result, err := b.callAsync(dumpMethod)
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

	_, err := b.callAsync(loadMethod, dump)
	return err
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
				}{js.Undefined(), fmt.Errorf(errorMsg)}
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
			fmt.Printf("🔍 JavaScript error details: %s\n", errorMsg)
		}

		done <- struct {
			result js.Value
			err    error
		}{js.Undefined(), fmt.Errorf(errorMsg)}
		return nil
	})
	defer catch.Release()

	// Attach handlers
	promise.Call("then", then).Call("catch", catch)

	// Wait for completion
	result := <-done
	return result.result, result.err
}
