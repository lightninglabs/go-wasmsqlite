//go:build js && wasm

package wasmsqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
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
	
	// VFS to use (default: "opfs-sahpool")
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
		VFS:         "opfs-sahpool",
		BusyTimeout: 5000,
		ParseTime:   false,
		WorkerURL:   "/bridge.worker.js", // Use direct file instead of blob
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
	
	if opts.VFS != "" && opts.VFS != "opfs-sahpool" {
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
	
	values, err := url.ParseQuery(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid DSN: %w", err)
	}
	
	if file := values.Get("file"); file != "" {
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

// createWorker creates a new Web Worker for the database connection
func createWorker(opts *Options) (js.Value, *internal.Queue, error) {
	var worker js.Value
	var err error
	
	if opts.WorkerURL != "" {
		// Use custom Worker URL
		worker = js.Global().Get("Worker").New(opts.WorkerURL)
	} else {
		// Use embedded Worker
		worker, err = createEmbeddedWorker()
		if err != nil {
			return js.Null(), nil, fmt.Errorf("failed to create embedded worker: %w", err)
		}
	}
	
	// Create request queue
	queue := internal.NewQueue(worker)
	
	// Initialize SQLite WASM in the Worker
	if err := initializeSQLiteWASM(queue); err != nil {
		queue.Close()
		return js.Null(), nil, fmt.Errorf("failed to initialize SQLite WASM: %w", err)
	}
	
	return worker, queue, nil
}

// createEmbeddedWorker creates a Worker from embedded JavaScript
func createEmbeddedWorker() (js.Value, error) {
	// Check if workerJS is available
	if len(workerJS) == 0 {
		return js.Null(), fmt.Errorf("embedded worker JS is empty")
	}
	
	// Create Blob from embedded Worker JavaScript
	uint8Array := js.Global().Get("Uint8Array").New(len(workerJS))
	js.CopyBytesToJS(uint8Array, workerJS)
	
	// Create array for blob constructor
	array := js.Global().Get("Array").New()
	array.Call("push", uint8Array)
	
	blob := js.Global().Get("Blob").New(array, map[string]interface{}{
		"type": "application/javascript",
	})
	
	// Check if blob was created successfully
	if blob.IsNull() {
		return js.Null(), fmt.Errorf("failed to create blob")
	}
	
	blobURL := js.Global().Get("URL").Call("createObjectURL", blob)
	blobURLStr := blobURL.String()
	
	if blobURLStr == "" || blobURLStr == "undefined" {
		return js.Null(), fmt.Errorf("failed to create blob URL")
	}
	
	// Create Worker from Blob URL
	worker := js.Global().Get("Worker").New(blobURL)
	
	// Don't revoke the URL immediately - let the Worker load first
	// We can revoke it later in a cleanup function
	
	return worker, nil
}

// initializeSQLiteWASM initializes the SQLite WASM module in the Worker
func initializeSQLiteWASM(queue *internal.Queue) error {
	// Create request to initialize SQLite WASM
	request := js.Global().Get("Object").New()
	request.Set("type", "init")
	
	// Send initialization request
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	response, err := queue.SendRequest(ctx, request)
	if err != nil {
		return err
	}
	
	if response.Error != nil {
		return response.Error
	}
	
	return nil
}

// openDatabase opens the database in the Worker
func openDatabase(queue *internal.Queue, opts *Options) error {
	request := createJSRequest(0, "open", map[string]interface{}{
		"file": opts.File,
		"vfs":  opts.VFS,
	})
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	response, err := queue.SendRequest(ctx, request)
	if err != nil {
		return err
	}
	
	if response.Error != nil {
		return response.Error
	}
	
	
	// Execute initial pragma statements if any
	if len(opts.Pragma) > 0 {
		for _, pragma := range opts.Pragma {
			if err := executePragma(queue, pragma); err != nil {
				return fmt.Errorf("failed to execute pragma %s: %w", pragma, err)
			}
		}
	}
	
	// Set journal mode if specified
	if opts.JournalMode != "" {
		pragma := fmt.Sprintf("PRAGMA journal_mode=%s", opts.JournalMode)
		if err := executePragma(queue, pragma); err != nil {
			return fmt.Errorf("failed to set journal mode: %w", err)
		}
	}
	
	// Set busy timeout
	if opts.BusyTimeout > 0 {
		pragma := fmt.Sprintf("PRAGMA busy_timeout=%d", opts.BusyTimeout)
		if err := executePragma(queue, pragma); err != nil {
			return fmt.Errorf("failed to set busy timeout: %w", err)
		}
	}
	
	return nil
}

// executePragma executes a pragma statement
func executePragma(queue *internal.Queue, pragma string) error {
	request := createJSRequest(0, "exec", map[string]interface{}{
		"sql":    pragma,
		"params": []driver.Value{},
	})
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	response, err := queue.SendRequest(ctx, request)
	if err != nil {
		return err
	}
	
	return response.Error
}