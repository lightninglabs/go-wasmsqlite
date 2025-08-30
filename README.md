# sqlc-wasm

A WebAssembly SQLite driver for Go that enables sqlc-generated code to run in the browser with OPFS persistence.

## Features

- 🚀 Run SQLite databases entirely in the browser
- 💾 Persistent storage using OPFS (Origin Private File System)
- 🔄 Full transaction support (BEGIN/COMMIT/ROLLBACK)
- ⚡ Works with any sqlc-generated SQLite code
- 📦 Direct SQLite WASM integration - uses official SQLite builds
- 🔍 VFS detection to know if using OPFS or in-memory storage
- 💼 Database dump/load functionality for backups and migrations
- 🏗️ Built-in Web Worker bridge for optimal performance
- 🌐 Cross-Origin Isolation support for SharedArrayBuffer

## Requirements

- Go 1.19+ with WASM support
- Modern browser with OPFS support (Chrome 102+, Firefox 111+, Safari 15.2+)
- HTTPS or localhost for OPFS access

## Installation

```bash
go get github.com/sputn1ck/sqlc-wasm
```

## Quick Start

```bash
# Setup and build everything
make setup
make build

# Run the demo
make serve
```

Visit http://localhost:8081 to see the demo in action.

## Usage

```go
import (
    "database/sql"
    _ "github.com/sputn1ck/sqlc-wasm"
)

func main() {
    // Open database with OPFS persistence
    db, err := sql.Open("wasmsqlite", "file=/myapp.db?vfs=opfs-sahpool")
    if err != nil {
        panic(err)
    }
    defer db.Close()
    
    // Use with sqlc-generated code as normal
    queries := database.New(db)
    // ... your queries here
}
```

## Project Structure

```
sqlc-wasm/
├── Makefile              # Build automation
├── go.mod & go.sum      # Go module files
├── *.go                 # Driver source files
├── bridge/              # JavaScript bridge
│   └── sqlite-bridge.js # Handcrafted bridge file
├── assets/              # SQLite WASM files (fetched)
│   ├── sqlite3.wasm
│   ├── sqlite3.js
│   ├── sqlite3-worker1.js
│   ├── sqlite3-worker1-promiser.js
│   └── sqlite3-opfs-async-proxy.js
├── scripts/             # Build scripts
│   └── fetch-sqlite-wasm.sh # Downloads SQLite WASM
└── example/             # Demo application
    ├── main.go          # Demo Go code
    ├── index.html       # Demo UI
    ├── server.js        # Dev server with CORS headers
    └── generated/       # SQLC generated code
```

## Setup

The project uses official SQLite WASM builds:

### 1. Download SQLite WASM Assets

```bash
# Fetch SQLite WASM from official source
make fetch-assets

# Or run the script directly
./scripts/fetch-sqlite-wasm.sh
```

This downloads SQLite WASM v3.50.4 and verifies its SHA3-256 checksum.

### 2. Build Your Application

```bash
# Build your Go WASM binary
GOOS=js GOARCH=wasm go build -o web/main.wasm ./cmd/app

# Copy Go's WASM support file
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" ./web/
```

### 3. Serve Files with Proper Headers

For OPFS and SharedArrayBuffer support, serve with these headers:

```
Cross-Origin-Embedder-Policy: require-corp
Cross-Origin-Opener-Policy: same-origin
```

## DSN Options

- `file` - Database file path (default: `/app.db`)
- `vfs` - Virtual file system (default: `opfs-sahpool`)
  - `opfs-sahpool` - Persistent storage using OPFS with SharedArrayBuffer pool
  - `opfs` - Standard OPFS storage
  - `:memory:` - In-memory database (no persistence)
- `busy_timeout` - Busy timeout in milliseconds (default: 5000)
- `mode` - Access mode (`ro`, `rw`, `rwc`, `memory`)
- `cache` - Cache mode (`shared`, `private`)

Example with options:
```go
db, err := sql.Open("wasmsqlite", "file=/data.db?vfs=opfs-sahpool&busy_timeout=10000&mode=rwc")
```

## Advanced Features

### Database Dump/Load

Export and import entire databases as SQL:

```go
import wasmsqlite "github.com/sputn1ck/sqlc-wasm"

// Export database
dump, err := wasmsqlite.DumpDatabase(db)
if err != nil {
    // handle error
}
// Save dump to localStorage, send to server, etc.

// Import database
err = wasmsqlite.LoadDatabase(db, dump)
if err != nil {
    // handle error
}
```

### VFS Detection

Check if database is using persistent storage:

```go
conn, _ := db.Conn(context.Background())
defer conn.Close()

var vfsType wasmsqlite.VFSType
conn.Raw(func(driverConn interface{}) error {
    c := driverConn.(*wasmsqlite.Conn)
    vfsType = c.GetVFSType()
    return nil
})

switch vfsType {
case wasmsqlite.VFSTypeOPFS:
    // Using persistent OPFS storage
case wasmsqlite.VFSTypeMemory:
    // Using in-memory storage
}
```

## Browser Compatibility

| Browser | Minimum Version | OPFS Support |
|---------|----------------|--------------|
| Chrome  | 102+          | ✅ Full      |
| Edge    | 102+          | ✅ Full      |
| Firefox | 111+          | ✅ Full      |
| Safari  | 15.2+         | ✅ Full      |

## Development

### Building

```bash
# Initial setup (fetches SQLite WASM)
make setup

# Build everything
make build
```

### Running Tests

```bash
make test
```

### Development Mode

```bash
# Build and serve with auto-reload
make dev
```

### Available Make Commands

```bash
make help              # Show all available commands
make setup            # Initial setup (fetch SQLite WASM)
make fetch-assets     # Download SQLite WASM from official source
make build            # Build everything
make build-wasm       # Build Go WASM only
make serve            # Run demo server
make test             # Run tests
make clean            # Clean build artifacts
```

## Architecture

```
┌─────────────────────────────────────────┐
│         Go Application (WASM)           │
│  ┌───────────────────────────────────┐  │
│  │     SQLC Generated Code           │  │
│  └───────────────────────────────────┘  │
│  ┌───────────────────────────────────┐  │
│  │     sqlc-wasm Driver              │  │
│  └───────────────────────────────────┘  │
└─────────────────────────────────────────┘
                    ↕
┌─────────────────────────────────────────┐
│   JavaScript Bridge (sqlite-bridge.js)  │
└─────────────────────────────────────────┘
                    ↕
┌─────────────────────────────────────────┐
│    SQLite Web Worker (Worker Thread)    │
│  ┌───────────────────────────────────┐  │
│  │  sqlite3-worker1-promiser.js      │  │
│  └───────────────────────────────────┘  │
│  ┌───────────────────────────────────┐  │
│  │     SQLite WASM (sqlite3.wasm)    │  │
│  └───────────────────────────────────┘  │
└─────────────────────────────────────────┘
                    ↕
┌─────────────────────────────────────────┐
│            OPFS Storage Layer           │
│         (Persistent File System)        │
└─────────────────────────────────────────┘
```

## Limitations

- SQLite extensions cannot be loaded dynamically
- Performance is slower than native SQLite (but optimized with Web Workers)
- OPFS storage is origin-scoped (per domain)
- Requires secure context (HTTPS/localhost)
- Cross-origin restrictions apply

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT

## Acknowledgments

- [SQLite](https://sqlite.org/) for the amazing database
- [@sqlite.org/sqlite-wasm](https://sqlite.org/wasm) for the WebAssembly build
- [sqlc](https://sqlc.dev/) for code generation