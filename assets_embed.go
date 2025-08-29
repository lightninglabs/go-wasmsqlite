//go:build js && wasm

package wasmsqlite

import _ "embed"

// Embedded assets for the Web Worker and SQLite WASM
// These are included in the Go binary when building for js/wasm

//go:embed assets/bridge.worker.js
var workerJS []byte

//go:embed assets/sqlite3.wasm
var sqliteWASM []byte

// Version information
const (
	// Version of this wasmsqlite driver
	Version = "0.1.0"
	
	// SQLiteVersion is the version of SQLite WASM being used
	SQLiteVersion = "3.46.1-build2"
)