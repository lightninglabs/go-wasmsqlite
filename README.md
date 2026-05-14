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

`sqlite-worker.js` imports `sqlite3.js`, calls `sqlite3InitModule()`, opens databases with `sqlite3.oo1.OpfsDb` for the default `opfs` VFS, and falls back to `:memory:` if OPFS is unavailable unless persistent storage is required. SQLite still runs in a Worker because OPFS requires worker-side file APIs.

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
    File:              "/myapp.db",
    VFS:               "opfs",
    RequirePersistent: true,
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
- `sqlite_js_url`: optional URL for `sqlite3.js`, used by `sqlite-worker.js`
- `require_persistent`: fail instead of falling back to memory when persistent storage is unavailable
- `parse_time`: parse SQLite timestamp strings into `time.Time` values for scans into `time.Time` or `sql.NullTime`

`parse_time=true` recognizes `time.RFC3339Nano`, `time.RFC3339`, SQLite-style datetime strings with optional fractional seconds and numeric offsets, `YYYY-MM-DD HH:MM:SS`, and `YYYY-MM-DD`.

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

`ExtractAssets` writes the runtime files in a flat layout:

```text
./static/wasm/sqlite-bridge.js
./static/wasm/sqlite-worker.js
./static/wasm/sqlite3.js
./static/wasm/sqlite3.wasm
./static/wasm/sqlite3-opfs-async-proxy.js
```

### Serve Assets

```go
handler := wasmsqlite.AssetHandler()
http.Handle("/wasm/", http.StripPrefix("/wasm", handler))
```

Available flat runtime paths include:

```text
/wasm/sqlite3.wasm
/wasm/sqlite3.js
/wasm/sqlite3-opfs-async-proxy.js
/wasm/sqlite-bridge.js
/wasm/sqlite-worker.js
```

`AssetHandler` also preserves the embedded `/assets/...` and `/bridge/...` paths for direct access, but the flat paths are the browser runtime layout. By default, `sqlite-worker.js` is resolved relative to the loaded `sqlite-bridge.js`, and `sqlite3.js` is resolved relative to `sqlite-worker.js`. If you serve files from unrelated directories, set both `worker_url` and `sqlite_js_url`.

Cross-origin isolation headers must be set on the app page, not only on SQLite assets. Wrap the whole app handler:

```go
app := wasmsqlite.WithCrossOriginIsolation(http.FileServer(http.Dir("./public")))
http.Handle("/", app)
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
    // In-memory storage; data will reset on refresh.
}
```

Use `RequirePersistent: true` or `require_persistent=true` when falling back to memory would be incorrect.

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

`make fetch-assets` reads the official SQLite download metadata and fetches the current `sqlite-wasm-*.zip` bundle with SHA3 verification. To pin a specific archive, pass `SQLITE_URL` and `EXPECTED_SHA`; `SQLITE_VERSION=3530100 make fetch-assets` selects that version when it is still listed on the download page.

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
