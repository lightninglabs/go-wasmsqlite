//go:build js && wasm

package wasmsqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall/js"
	"time"
)

// Options represents configuration options for opening a wasmsqlite database
type Options struct {
	// File path for the database (default: "/app.db")
	File string

	// VFS to use (default: "auto", which tries opfs-wl, opfs-sahpool, opfs,
	// then memory unless persistent storage is required).
	VFS string

	// Busy timeout in milliseconds (default: 5000)
	BusyTimeout int

	// Custom sqlite-worker.js URL. Empty uses "sqlite-worker.js" relative to
	// the app page.
	WorkerURL string

	// Custom sqlite3.js URL used by sqlite-worker.js. Empty uses "sqlite3.js"
	// relative to sqlite-worker.js.
	SQLiteJSURL string

	// Require OPFS/persistent storage instead of falling back to memory.
	RequirePersistent bool

	// DisallowMemory fails open instead of falling back to memory. It is an
	// explicit alias for RequirePersistent for callers who care about storage
	// durability rather than the exact persistence mechanism.
	DisallowMemory bool

	// Whether to parse time strings as time.Time (default: false).
	ParseTime bool

	// Journal mode (default: not set, uses SQLite default)
	JournalMode string

	// SQLite open mode query value: ro, rw, rwc, or memory.
	Mode string

	// SQLite URI cache query value.
	Cache string

	// Custom pragma statements to execute on open
	Pragma []string
}

// DefaultOptions returns default options for opening a database
func DefaultOptions() *Options {
	return &Options{
		File:        "/app.db",
		VFS:         "auto",
		BusyTimeout: 5000,
		ParseTime:   false,
		WorkerURL:   "",
		SQLiteJSURL: "",
	}
}

// Open opens a database with the given options
func Open(opts *Options) (*sql.DB, error) {
	if opts == nil {
		opts = DefaultOptions()
	}

	// Build DSN from options
	dsn := buildDSN(opts)

	db, err := sql.Open("wasmsqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// buildDSN builds a DSN string from options
func buildDSN(opts *Options) string {
	values := url.Values{}

	if opts.File != "" && opts.File != "/app.db" {
		values.Set("file", opts.File)
	}

	if opts.VFS != "" && opts.VFS != "auto" {
		values.Set("vfs", opts.VFS)
	}

	if opts.BusyTimeout != 0 && opts.BusyTimeout != 5000 {
		values.Set("busy_timeout", strconv.Itoa(opts.BusyTimeout))
	}

	if opts.WorkerURL != "" {
		values.Set("worker_url", opts.WorkerURL)
	}

	if opts.SQLiteJSURL != "" {
		values.Set("sqlite_js_url", opts.SQLiteJSURL)
	}

	if opts.RequirePersistent {
		values.Set("require_persistent", "true")
	}

	if opts.DisallowMemory {
		values.Set("disallow_memory", "true")
	}

	if opts.ParseTime {
		values.Set("parse_time", "true")
	}

	if opts.JournalMode != "" {
		values.Set("journal_mode", opts.JournalMode)
	}

	if opts.Mode != "" {
		values.Set("mode", opts.Mode)
	}

	if opts.Cache != "" {
		values.Set("cache", opts.Cache)
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

	values, err := url.ParseQuery(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid DSN: %w", err)
	}

	if file := values.Get("file"); file != "" {
		if questionMark := strings.Index(file, "?"); questionMark != -1 {
			nestedValues, err := url.ParseQuery(file[questionMark+1:])
			if err != nil {
				return nil, fmt.Errorf("invalid file query parameters: %w", err)
			}
			for key, nested := range nestedValues {
				if _, exists := values[key]; !exists {
					values[key] = nested
				}
			}
			file = file[:questionMark]
		}
		opts.File = file
	}

	if vfs := values.Get("vfs"); vfs != "" {
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

	if sqliteJSURL := values.Get("sqlite_js_url"); sqliteJSURL != "" {
		opts.SQLiteJSURL = sqliteJSURL
	}

	if requirePersistent := values.Get("require_persistent"); requirePersistent == "true" {
		opts.RequirePersistent = true
	}

	if disallowMemory := values.Get("disallow_memory"); disallowMemory == "true" {
		opts.DisallowMemory = true
	}

	if parseTime := values.Get("parse_time"); parseTime == "true" {
		opts.ParseTime = true
	}

	if journalMode := values.Get("journal_mode"); journalMode != "" {
		opts.JournalMode = journalMode
	}

	if mode := values.Get("mode"); mode != "" {
		opts.Mode = mode
	}

	if cache := values.Get("cache"); cache != "" {
		opts.Cache = cache
	}

	if pragma := values.Get("pragma"); pragma != "" {
		opts.Pragma = strings.Split(pragma, ";")
	}

	return opts, nil
}

// createWorker initializes the JavaScript bridge that owns the SQLite worker.
func createWorker(opts *Options) (js.Value, error) {
	// Check if the SQLite bridge is available
	bridge := js.Global().Get("sqliteBridge")
	if bridge.IsUndefined() {
		return js.Null(), fmt.Errorf("%w: ensure sqlite-bridge.js is loaded", ErrBridgeNotLoaded)
	}

	// Initialize SQLite WASM through the bridge
	if err := initializeSQLiteBridge(bridge, opts); err != nil {
		return js.Null(), fmt.Errorf("failed to initialize SQLite bridge: %w", err)
	}

	return bridge, nil
}

// initializeSQLiteBridge initializes the SQLite bridge
func initializeSQLiteBridge(bridge js.Value, opts *Options) error {
	initMethod := bridge.Get("init")
	if initMethod.IsUndefined() {
		return fmt.Errorf("sqliteBridge.init method not found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	args := []interface{}{}
	if opts.WorkerURL != "" || opts.SQLiteJSURL != "" {
		options := js.Global().Get("Object").New()
		if opts.WorkerURL != "" {
			options.Set("workerURL", opts.WorkerURL)
		}
		if opts.SQLiteJSURL != "" {
			options.Set("sqliteJSURL", opts.SQLiteJSURL)
		}
		args = append(args, options)
	}

	promise := initMethod.Invoke(args...)
	if promise.IsUndefined() {
		return fmt.Errorf("bridge.init() did not return a promise")
	}

	// Wait for the promise to resolve
	done := make(chan error, 1)
	var then js.Func
	var catch js.Func
	var releaseOnce sync.Once
	releaseCallbacks := func() {
		releaseOnce.Do(func() {
			then.Release()
			catch.Release()
		})
	}

	checkProtocol := func(result js.Value, field string) error {
		value := result.Get(field)
		if value.IsUndefined() {
			return fmt.Errorf("%w: missing %s", ErrProtocolMismatch, field)
		}
		if got := value.Int(); got != ProtocolVersion {
			return fmt.Errorf("%w: %s expected %d, got %d", ErrProtocolMismatch, field, ProtocolVersion, got)
		}
		return nil
	}

	// Handle promise resolution
	then = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer releaseCallbacks()
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
					done <- fmt.Errorf("%w: bridge initialization failed", ErrWorkerInitFailed)
					return nil
				}
			}
			if !result.IsUndefined() {
				if err := checkProtocol(result, "bridgeProtocolVersion"); err != nil {
					done <- err
					return nil
				}
				if err := checkProtocol(result, "workerProtocolVersion"); err != nil {
					done <- err
					return nil
				}
			}
		}

		done <- nil
		return nil
	})

	// Handle promise rejection
	catch = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer releaseCallbacks()
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("promise catch handler panicked: %v", r)
			}
		}()

		if len(args) > 0 {
			err := args[0]
			done <- fmt.Errorf("%w: %w", ErrWorkerInitFailed, classifyBridgeError(err.String()))
		} else {
			done <- fmt.Errorf("%w: unknown error", ErrWorkerInitFailed)
		}
		return nil
	})

	// Attach handlers
	promise.Call("then", then).Call("catch", catch)

	// Wait for completion or timeout
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
