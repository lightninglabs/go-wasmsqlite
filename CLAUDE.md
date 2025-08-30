# Claude Developer Guide for sqlc-wasm

## Project Overview
WebAssembly SQLite driver for Go that enables sqlc-generated code to run in browsers with OPFS persistence. Users can write standard Go database code that runs entirely client-side.

## Core Architecture
```
Go App (WASM) → Go Driver → JS Bridge → SQLite Worker → OPFS Storage
```

## Directory Structure
```
sqlc-wasm/
├── bridge/                 # JavaScript bridge between Go and SQLite WASM
│   └── sqlite-bridge.js   # Handcrafted bridge file
│
├── assets/                # SQLite WASM files (fetched from official source)
│   ├── sqlite3.js
│   ├── sqlite3.wasm
│   ├── sqlite3-worker1.js
│   ├── sqlite3-worker1-promiser.js
│   └── sqlite3-opfs-async-proxy.js
│
├── scripts/               # Build and utility scripts
│   └── fetch-sqlite-wasm.sh  # Downloads and verifies SQLite WASM
│
├── example/               # Demo application
│   ├── main.go           # Example Go code using the driver
│   ├── index.html        # Demo UI
│   ├── generated/        # sqlc-generated database code
│   └── [JS and WASM files copied here during build]
│
├── *.go                   # Driver implementation files
│   ├── driver.go         # Main driver implementation
│   ├── conn.go           # Connection handling
│   ├── stmt.go           # Statement execution
│   ├── rows.go           # Result set handling
│   ├── tx.go             # Transaction support
│   ├── bridge_adapter.go # Adapter for JS bridge
│   └── open.go           # Database opening logic
│
└── Makefile              # Build automation
```

## Key Components

### 1. Bridge (`bridge/sqlite-bridge.js`)
- **Purpose**: Handcrafted JavaScript bridge between Go and SQLite WASM
- **Architecture**: Direct integration with SQLite WASM Worker API
- **No npm dependencies**: Simple, standalone JS file

### 2. Go Driver (`*.go`)
- **Purpose**: Implements `database/sql` driver interface
- **Key file**: `bridge_adapter.go` - Calls JS bridge via `syscall/js`
- **Usage**: `sql.Open("wasmsqlite", "file=/app.db?vfs=opfs")`

### 3. SQLite Assets (`assets/`)
- **Source**: Official SQLite WASM distribution (v3.50.4)
- **Fetched via**: `scripts/fetch-sqlite-wasm.sh`
- **Verification**: SHA3-256 checksum validation
- **Files**: Core SQLite WASM, workers, and OPFS proxy

### 4. Example (`example/`)
- **Purpose**: Demonstrates driver usage with sqlc-generated code
- **Note**: Bridge and assets are copied here during build
- **Server**: Requires CORS headers for OPFS support

## Build Commands
```bash
make setup          # Initial setup (fetch SQLite WASM assets for development)
make fetch-assets   # Download SQLite WASM from official source
make build          # Build everything (assets → wasm → example)
make build-wasm     # Build just the Go WASM
make serve          # Build and serve demo at localhost:8081
make clean          # Clean all build artifacts
```

## Embedded Assets
SQLite WASM assets are embedded in the Go module using `//go:embed`. Users can:
- Extract assets to filesystem: `wasmsqlite.ExtractAssets("./static")`
- Serve via HTTP handler: `http.Handle("/wasm/", wasmsqlite.AssetHandler())`
- Access individual files: `wasmsqlite.GetSQLiteWASM()`

## Common Tasks

### Modify the Bridge
1. Edit `bridge/sqlite-bridge.js`
2. Run `make build`
3. Test with `make serve`

### Update SQLite Version
1. Edit `SQLITE_VERSION` in `scripts/fetch-sqlite-wasm.sh`
2. Update SHA3-256 checksum in the script
3. Run `make fetch-assets`
4. Commit the updated assets to the repository
5. Tag a new version with embedded assets

### Add Driver Features
1. Modify relevant Go files (likely `conn.go`, `stmt.go`, or `rows.go`)
2. Update `bridge_adapter.go` if new JS bridge methods needed
3. Test with `make build && make serve`

### Debug Issues
- Check browser console for JS errors
- Look for `🔍` prefixed debug messages from bridge_adapter
- Verify CORS headers are set (required for OPFS)
- Check if running on HTTPS/localhost (required for OPFS)

## Important Notes

### Web Worker Architecture
SQLite WASM uses Web Workers with these files:
- `sqlite-bridge.js` - Our handcrafted bridge
- `sqlite3.js` - Main SQLite JavaScript API
- `sqlite3-worker1.js` - SQLite worker
- `sqlite3-worker1-promiser.js` - Promise-based worker interface
- `sqlite3-opfs-async-proxy.js` - OPFS proxy worker
- `sqlite3.wasm` - WebAssembly binary

**All files must be served** from the same directory.

### OPFS Requirements
- HTTPS or localhost only
- Required headers:
  - `Cross-Origin-Opener-Policy: same-origin`
  - `Cross-Origin-Embedder-Policy: require-corp`

### VFS Fallback
If OPFS unavailable, automatically falls back to in-memory storage.

### sqlc Integration
Example uses sqlc-generated code in `example/generated/`:
- `schema.sql` - Database schema
- `queries.sql` - SQL queries
- `sqlc.yaml` - sqlc configuration

## Testing Workflow
1. `make clean` - Start fresh
2. `make build` - Build everything
3. `make serve` - Start demo server
4. Visit `http://localhost:8081` via playwright mcp
5. Open browser console to see debug output
6. Click buttons to test CRUD operations

## File Dependencies
- `assets/*` must be fetched before building
- `example/main.wasm` depends on all Go source files
- HTML expects these files in same directory:
  - `sqlite-bridge.js`
  - `sqlite3.js`, `sqlite3-worker1.js`, `sqlite3-worker1-promiser.js`
  - `sqlite3-opfs-async-proxy.js`
  - `sqlite3.wasm`
  - `main.wasm`, `wasm_exec.js`

## Gotchas
- Don't try to bundle all JS into single file (Web Workers need separate files)
- Bridge must be loaded before Go WASM tries to open database
- Column name detection in bridge is heuristic-based (check `bridge/sqlite-bridge.js`)
- Transaction isolation is limited by SQLite's capabilities in WASM
- SHA3-256 verification requires `sha3sum` or `openssl` with SHA3 support