# sqlc-wasm

A WebAssembly SQLite driver for Go that enables sqlc-generated code to run in the browser with OPFS persistence.

## Features

- 🚀 Run SQLite databases entirely in the browser
- 💾 Persistent storage using OPFS (Origin Private File System)
- 🔄 Full transaction support (BEGIN/COMMIT/ROLLBACK)
- ⚡ Works with any sqlc-generated SQLite code
- 📦 No JavaScript toolchain required for users
- 🔍 VFS detection to know if using OPFS or in-memory storage
- 💼 Database dump/load functionality for backups and migrations
- 🏗️ Embedded Worker - no external JavaScript files needed

## Requirements

- Go 1.19+ with WASM support
- Modern browser with OPFS support (Chrome 102+, Firefox 111+, Safari 15.2+)
- HTTPS or localhost for OPFS access

## Installation

```bash
go get github.com/sputn1ck/sqlc-wasm
```

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

## Important Setup Steps

### 1. Obtain SQLite WASM

Download the official SQLite WASM file from the SQLite project:

```bash
# Download SQLite WASM (latest version)
curl -L https://sqlite.org/2024/sqlite-wasm-3460000.zip -o sqlite-wasm.zip
unzip sqlite-wasm.zip
cp sqlite-wasm-*/jswasm/sqlite3.wasm ./web/

# Or use npm/CDN
npm install @sqlite.org/sqlite-wasm
cp node_modules/@sqlite.org/sqlite-wasm/sqlite3.wasm ./web/
```

### 2. Build Your Application

```bash
# Build your Go WASM binary
GOOS=js GOARCH=wasm go build -o web/main.wasm ./cmd/app

# Copy Go's WASM support file
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" ./web/
```

### 3. Serve Files Correctly

Your web server must serve these files:
- `main.wasm` - Your Go application
- `wasm_exec.js` - Go's WASM support
- `sqlite3.wasm` - SQLite WebAssembly (MUST be at root path `/sqlite3.wasm`)

### 4. Use HTTPS or localhost

OPFS requires a secure context:
- ✅ `https://` - Production
- ✅ `http://localhost` - Development
- ❌ `http://127.0.0.1` - Won't work
- ❌ `http://[::]:8080` - Won't work

## Example

See the `example/` directory for a complete working demo:

```bash
cd example
make serve  # Serves on http://localhost:8081
```

## DSN Options

- `file` - Database file path (default: `/app.db`)
- `vfs` - Virtual file system (default: `opfs-sahpool`)
  - `opfs-sahpool` - Persistent storage using OPFS
  - `:memory:` - In-memory database (no persistence)
- `busy_timeout` - Busy timeout in milliseconds (default: 5000)
- `journal_mode` - Journal mode (e.g., `wal`, `delete`)

Example with options:
```go
db, err := sql.Open("wasmsqlite", "file=/data.db?vfs=opfs-sahpool&busy_timeout=10000&journal_mode=wal")
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

## Limitations

- SQLite extensions cannot be loaded dynamically
- Performance is slower than native SQLite
- OPFS storage is origin-scoped (per domain)
- Requires secure context (HTTPS/localhost)

## Development

To modify the Worker or contribute:

```bash
# Install dependencies
cd worker
npm install

# Build Worker
npm run build

# Run tests
cd ../example
make serve
```

## License

MIT

## Acknowledgments

- [SQLite](https://sqlite.org/) for the amazing database
- [@sqlite.org/sqlite-wasm](https://sqlite.org/wasm) for the WebAssembly build
- [sqlc](https://sqlc.dev/) for code generation