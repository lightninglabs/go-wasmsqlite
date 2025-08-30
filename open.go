//go:build js && wasm

package wasmsqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"syscall/js"
	"time"

	"github.com/sputn1ck/sqlc-wasm/internal"
)

// Options represents configuration options for opening a wasmsqlite database
type Options struct {
	// File path for the database (default: "/app.db")
	File string

	// VFS to use (default: "opfs")
	VFS string

	// Busy timeout in milliseconds (default: 5000)
	BusyTimeout int

	// Custom Worker URL (optional, overrides embedded Worker)
	WorkerURL string

	// Whether to parse time strings as time.Time (default: false)
	ParseTime bool

	// Journal mode (default: not set, uses SQLite default)
	JournalMode string

	// Custom pragma statements to execute on open
	Pragma []string
}

// DefaultOptions returns default options for opening a database
func DefaultOptions() *Options {
	return &Options{
		File:        "/app.db",
		VFS:         "opfs",
		BusyTimeout: 5000,
		ParseTime:   false,
		WorkerURL:   "", // Empty means use embedded Worker
	}
}

// Open opens a database with the given options
func Open(opts *Options) (*sql.DB, error) {
	if opts == nil {
		opts = DefaultOptions()
	}

	// Build DSN from options
	dsn := buildDSN(opts)

	return sql.Open("wasmsqlite", dsn)
}

// buildDSN builds a DSN string from options
func buildDSN(opts *Options) string {
	values := url.Values{}

	if opts.File != "" && opts.File != "/app.db" {
		values.Set("file", opts.File)
	}

	if opts.VFS != "" && opts.VFS != "opfs" {
		values.Set("vfs", opts.VFS)
	}

	if opts.BusyTimeout != 0 && opts.BusyTimeout != 5000 {
		values.Set("busy_timeout", strconv.Itoa(opts.BusyTimeout))
	}

	if opts.WorkerURL != "" {
		values.Set("worker_url", opts.WorkerURL)
	}

	if opts.ParseTime {
		values.Set("parse_time", "true")
	}

	if opts.JournalMode != "" {
		values.Set("journal_mode", opts.JournalMode)
	}

	if len(opts.Pragma) > 0 {
		values.Set("pragma", strings.Join(opts.Pragma, ";"))
	}

	if len(values) == 0 {
		return ""
	}

	return values.Encode()
}

// parseDSN parses a DSN string into options
func parseDSN(dsn string) (*Options, error) {
	opts := DefaultOptions()

	if dsn == "" {
		return opts, nil
	}

	fmt.Printf("🔍 Parsing DSN: %s\n", dsn)

	values, err := url.ParseQuery(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid DSN: %w", err)
	}

	if file := values.Get("file"); file != "" {
		// Remove any query parameters from the file path
		if questionMark := strings.Index(file, "?"); questionMark != -1 {
			file = file[:questionMark]
		}
		fmt.Printf("🔍 Extracted file: %s\n", file)
		opts.File = file
	}

	if vfs := values.Get("vfs"); vfs != "" {
		fmt.Printf("🔍 Extracted VFS: %s\n", vfs)
		opts.VFS = vfs
	}

	if timeout := values.Get("busy_timeout"); timeout != "" {
		t, err := strconv.Atoi(timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid busy_timeout: %w", err)
		}
		opts.BusyTimeout = t
	}

	if workerURL := values.Get("worker_url"); workerURL != "" {
		opts.WorkerURL = workerURL
	}

	if parseTime := values.Get("parse_time"); parseTime == "true" {
		opts.ParseTime = true
	}

	if journalMode := values.Get("journal_mode"); journalMode != "" {
		opts.JournalMode = journalMode
	}

	if pragma := values.Get("pragma"); pragma != "" {
		opts.Pragma = strings.Split(pragma, ";")
	}

	return opts, nil
}

// createWorker creates a new bridge to SQLite WASM
func createWorker(opts *Options) (js.Value, *internal.Queue, error) {
	// Check if the SQLite bridge is available
	bridge := js.Global().Get("sqliteBridge")
	if bridge.IsUndefined() {
		return js.Null(), nil, fmt.Errorf("sqliteBridge not found - ensure sqlite-bridge.js is loaded")
	}

	// Initialize SQLite WASM through the bridge
	if err := initializeSQLiteBridge(bridge); err != nil {
		return js.Null(), nil, fmt.Errorf("failed to initialize SQLite bridge: %w", err)
	}

	// Return the bridge itself as the "worker" and a nil queue
	// We'll bypass the queue system entirely
	return bridge, nil, nil
}

// initializeSQLiteBridge initializes the SQLite bridge
func initializeSQLiteBridge(bridge js.Value) error {
	// The bridge should already be initialized when it was loaded
	// We can just check if the init method exists
	initMethod := bridge.Get("init")
	if initMethod.IsUndefined() {
		return fmt.Errorf("sqliteBridge.init method not found")
	}

	// Call init and wait for it to complete
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a promise that we can await
	promise := initMethod.Invoke()
	if promise.IsUndefined() {
		return fmt.Errorf("bridge.init() did not return a promise")
	}

	// Wait for the promise to resolve
	done := make(chan error, 1)

	// Handle promise resolution
	then := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("promise then handler panicked: %v", r)
			}
		}()

		// Check if result indicates success
		if len(args) > 0 {
			result := args[0]
			if !result.IsUndefined() && !result.Get("ok").IsUndefined() {
				if !result.Get("ok").Bool() {
					done <- fmt.Errorf("bridge initialization failed")
					return nil
				}
			}
		}

		done <- nil
		return nil
	})
	defer then.Release()

	// Handle promise rejection
	catch := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("promise catch handler panicked: %v", r)
			}
		}()

		if len(args) > 0 {
			err := args[0]
			done <- fmt.Errorf("bridge initialization failed: %s", err.String())
		} else {
			done <- fmt.Errorf("bridge initialization failed with unknown error")
		}
		return nil
	})
	defer catch.Release()

	// Attach handlers
	promise.Call("then", then).Call("catch", catch)

	// Wait for completion or timeout
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("bridge initialization timed out")
	}
}
