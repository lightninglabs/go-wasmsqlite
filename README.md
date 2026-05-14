# go-wasmsqlite

A WebAssembly SQLite driver for Go. It lets browser-based Go WASM applications use the standard `database/sql` API with SQLite running entirely client-side and persisting to OPFS when the browser supports it.

## Current Architecture

The runtime uses SQLite's supported object-oriented WASM API directly. The deprecated SQLite Worker1/Promiser API is not used at runtime.

```
Go App (WASM)
    -> database/sql driver ("wasmsqlite")
    -> syscall/js adapter
    -> sqlite-bridge.js RPC client on the main thread
    -> sqlite-worker.js dedicated Worker
    -> sqlite3.oo1.DB / sqlite3.oo1.OpfsDb
    -> SQLite WASM + OPFS storage
```

`sqlite-worker.js` imports `sqlite3.js`, calls `sqlite3InitModule()`, opens databases with `sqlite3.oo1.OpfsDb` for the default `opfs` VFS, and falls back to `:memory:` if OPFS is unavailable. SQLite still runs in a Worker because OPFS requires worker-side file APIs.

## Features

- Standard `database/sql` driver named `wasmsqlite`
- OPFS persistence in supported browsers
- In-memory fallback when OPFS is unavailable
- Transactions through `BEGIN IMMEDIATE`, `COMMIT`, and `ROLLBACK`
- BLOB round trips through `[]byte`
- Database dump/load helpers with BLOB-safe SQL literals
- Embedded SQLite WASM and bridge assets
- Optional `golang-migrate` driver support

## Requirements

- Go 1.24+ for this module
- A modern browser with WebAssembly and OPFS support
- HTTPS or localhost for OPFS
- Cross-origin isolation for OPFS support:
  - `Cross-Origin-Opener-Policy: same-origin`
  - `Cross-Origin-Embedder-Policy: require-corp` or `credentialless`

The example includes `enable-threads.js`, a service worker that adds these headers for static hosts such as GitHub Pages.

## Installation

```bash
go get github.com/sputn1ck/go-wasmsqlite
```

## Usage

Use the driver through `database/sql`:

```go
import (
    "database/sql"

    _ "github.com/sputn1ck/go-wasmsqlite"
)

func main() {
    db, err := sql.Open("wasmsqlite", "file=/myapp.db?vfs=opfs&busy_timeout=5000")
    if err != nil {
        panic(err)
    }
    defer db.Close()

    // Recommended for every OPFS database handle.
    db.SetMaxOpenConns(1)

    if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS notes (id INTEGER PRIMARY KEY, body TEXT)`); err != nil {
        panic(err)
    }
}
```

The convenience helper sets the OPFS connection limit for you:

```go
db, err := wasmsqlite.Open(&wasmsqlite.Options{
    File: "/myapp.db",
    VFS:  "opfs",
})
```

Direct `sql.Open(...)` callers should call `db.SetMaxOpenConns(1)` for each OPFS database. The worker rejects duplicate opens of the same OPFS filename in one worker, and multiple concurrent database handles are not useful for this browser storage model.

## DSN Options

- `file`: database filename, default `/app.db`
- `vfs`: SQLite VFS, default `opfs`
- `busy_timeout`: busy timeout in milliseconds, default `5000`
- `mode`: SQLite URI mode such as `ro`, `rw`, `rwc`, or `memory`
- `cache`: SQLite URI cache mode such as `shared` or `private`
- `journal_mode`: runs `PRAGMA journal_mode=<value>` after open
- `pragma`: semicolon-separated pragmas to run after open
- `worker_url`: optional URL for `sqlite-worker.js`
- `parse_time`: accepted for DSN compatibility

Supported VFS values:

- `opfs`: default persistent OPFS database via `sqlite3.oo1.OpfsDb`
- `opfs-sahpool`: explicit SQLite SAH pool VFS when available
- `memory`: in-memory database

`file=:memory:` also opens an in-memory database.

## Assets

SQLite WASM assets are embedded with `//go:embed`. The runtime needs these files to be served from the same directory in a browser app:

- `sqlite-bridge.js`
- `sqlite-worker.js`
- `sqlite3.js`
- `sqlite3.wasm`
- `sqlite3-opfs-async-proxy.js`
- Go's `wasm_exec.js`
- your Go WASM binary

The upstream SQLite Worker1 files are intentionally not fetched or published because this project no longer loads them at runtime.

### Extract Assets

```go
err := wasmsqlite.ExtractAssets("./static/wasm")
if err != nil {
    log.Fatal(err)
}
```

### Serve Assets

```go
handler := wasmsqlite.AssetHandler()
http.Handle("/wasm/", http.StripPrefix("/wasm", handler))
```

Available paths include:

```text
/wasm/assets/sqlite3.wasm
/wasm/assets/sqlite3.js
/wasm/assets/sqlite3-opfs-async-proxy.js
/wasm/bridge/sqlite-bridge.js
/wasm/bridge/sqlite-worker.js
```

### Access Individual Assets

```go
wasmBytes, _ := wasmsqlite.GetSQLiteWASM()
sqliteJS, _ := wasmsqlite.GetSQLiteJS()
bridgeJS, _ := wasmsqlite.GetBridgeJS()
workerJS, _ := wasmsqlite.GetWorkerJS()
```

## Example App

Build and serve the demo:

```bash
make build-example
make serve
```

Then open `http://localhost:8081`.

The example uses:

- `example/index.html`
- `example/enable-threads.js` for static-host cross-origin isolation
- `example/main.go` for the Go WASM app
- `example/migrations/` with `golang-migrate`
- generated `database/sql` query code in `example/generated/`

`make build-example` copies the runtime browser files into `example/`, which is also what the GitHub Pages workflow publishes.

## Database Dump/Load

```go
dump, err := wasmsqlite.DumpDatabase(db)
if err != nil {
    return err
}

if err := wasmsqlite.LoadDatabase(db, dump); err != nil {
    return err
}
```

Dump output uses hex literals for BLOB values, so `[]byte` data can be restored safely.

## VFS Detection

```go
conn, err := db.Conn(context.Background())
if err != nil {
    return err
}
defer conn.Close()

vfsType, err := wasmsqlite.GetVFSType(conn)
if err != nil {
    return err
}

switch vfsType {
case wasmsqlite.VFSTypeOPFS:
    // Persistent OPFS storage.
case wasmsqlite.VFSTypeMemory:
    // In-memory storage.
}
```

## Migrations

The package includes a `golang-migrate` database driver that runs migrations against an existing `*sql.DB`:

```go
sourceDriver, err := iofs.New(migrationFS, "migrations")
if err != nil {
    return err
}

dbDriver, err := wasmsqlite.NewMigrateDriver(db)
if err != nil {
    return err
}

m, err := migrate.NewWithInstance("iofs", sourceDriver, "wasmsqlite", dbDriver)
if err != nil {
    return err
}

return m.Up()
```

## Development

```bash
make setup          # Fetch SQLite WASM assets
make build          # Fetch assets, build root WASM, and build example
make build-example  # Build only the demo app and copy browser runtime files
make serve          # Serve demo at http://localhost:8081
make test           # Run normal Go tests; browser tests are opt-in
make browser-test   # Run headless Chrome browser E2E tests
make clean          # Remove build artifacts
```

Browser tests build a WASM test binary, serve the SQLite assets with the required headers, launch headless Chrome, and verify OPFS persistence, BLOBs, dump/load, transactions, memory mode, migrations, and the static Pages-style example path.

## GitHub Pages

The Pages workflow runs `make build-example` and publishes `./example`. The published directory must contain:

- `index.html`
- `enable-threads.js`
- `main.wasm`
- `wasm_exec.js`
- `sqlite-bridge.js`
- `sqlite-worker.js`
- `sqlite3.js`
- `sqlite3.wasm`
- `sqlite3-opfs-async-proxy.js`

`enable-threads.js` is loaded before the Go WASM app. On static hosts without COOP/COEP response headers, it registers a service worker, reloads the page, and makes the page cross-origin isolated so OPFS can work.

## Limitations

- SQLite extensions cannot be loaded dynamically.
- OPFS storage is origin-scoped.
- OPFS requires HTTPS or localhost.
- If OPFS is unavailable, `vfs=opfs` falls back to in-memory storage.

## License

MIT

## Acknowledgments

- [SQLite](https://sqlite.org/) for SQLite and the official WASM build
- [database/sql](https://pkg.go.dev/database/sql) for the standard Go SQL API
