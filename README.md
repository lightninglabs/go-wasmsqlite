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

`sqlite-worker.js` imports `sqlite3.js`, calls `sqlite3InitModule()`, opens databases with an automatic OPFS preference order by default (`opfs-wl`, `opfs-sahpool`, then `opfs`), and falls back to `:memory:` only when memory fallback is allowed. SQLite still runs in a Worker because OPFS requires worker-side file APIs.

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
    db, err := sql.Open("wasmsqlite", "file=/myapp.db?vfs=auto&busy_timeout=5000")
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
    File:           "/myapp.db",
    VFS:            "auto",
    DisallowMemory: true,
})
```

Direct `sql.Open(...)` callers should call `db.SetMaxOpenConns(1)` for each OPFS database. The worker rejects duplicate opens of the same OPFS filename in one worker, and multiple concurrent database handles are not useful for this browser storage model.

Named SQLite parameters are supported through `sql.Named`:

```go
_, err := db.Exec(
    `INSERT INTO notes(id, body) VALUES (:id, $body)`,
    sql.Named("id", 1),
    sql.Named("body", "hello"),
)
```

Do not mix positional and named parameters in one call.

`ExecContext`, `QueryContext`, `BeginTx`, `Ping`, dump, and load respect context cancellation while waiting for the JavaScript bridge. Canceling a Go context stops waiting on the Go side; it does not forcibly interrupt a SQLite operation that has already been posted to the Worker.

## DSN Options

- `file`: database filename, default `/app.db`
- `vfs`: SQLite VFS, default `auto`
- `busy_timeout`: busy timeout in milliseconds, default `5000`
- `mode`: SQLite URI mode such as `ro`, `rw`, `rwc`, or `memory`
- `cache`: SQLite URI cache mode such as `shared` or `private`
- `journal_mode`: runs `PRAGMA journal_mode=<value>` after open
- `pragma`: semicolon-separated pragmas to run after open
- `worker_url`: optional URL for `sqlite-worker.js`
- `sqlite_js_url`: optional URL for `sqlite3.js`, used by `sqlite-worker.js`
- `require_persistent`: fail instead of falling back to memory when persistent storage is unavailable
- `disallow_memory`: explicit alias for `require_persistent`; fail instead of falling back to memory
- `parse_time`: parse SQLite timestamp strings into `time.Time` values for scans into `time.Time` or `sql.NullTime`

`parse_time=true` recognizes `time.RFC3339Nano`, `time.RFC3339`, SQLite-style datetime strings with optional fractional seconds and numeric offsets, `YYYY-MM-DD HH:MM:SS`, and `YYYY-MM-DD`.

Supported VFS values:

- `auto`: default; tries `opfs-wl`, then `opfs-sahpool`, then `opfs`, then memory if allowed
- `opfs`: persistent OPFS database via `sqlite3.oo1.OpfsDb`; falls back to memory if unavailable and memory is allowed
- `opfs-wl`: OPFS database using SQLite's Web Locks VFS for fairer multi-tab lock scheduling when the browser supports `Atomics.waitAsync`
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

## Production Serving

A production deployment should serve one flat runtime directory:

```text
public/
  index.html
  main.wasm
  wasm_exec.js
  sqlite-bridge.js
  sqlite-worker.js
  sqlite3.js
  sqlite3.wasm
  sqlite3-opfs-async-proxy.js
```

Minimal Go static server:

```go
package main

import (
    "log"
    "net/http"

    "github.com/sputn1ck/go-wasmsqlite"
)

func main() {
    app := http.FileServer(http.Dir("./public"))
    handler := wasmsqlite.WithCrossOriginIsolation(app)

    log.Fatal(http.ListenAndServe(":8080", handler))
}
```

`WithCrossOriginIsolation` sets COOP/COEP on every response and `application/wasm` for `.wasm` files. Static hosts must do the same for the app page. For caching, prefer hash-named immutable files; otherwise deploy `main.wasm`, `sqlite-bridge.js`, `sqlite-worker.js`, `sqlite3.js`, `sqlite3.wasm`, and `sqlite3-opfs-async-proxy.js` together. The bridge and worker include a small protocol check so mismatched runtime files fail clearly instead of producing opaque worker errors.

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
storageInfo, err := wasmsqlite.GetStorageInfo(db)
if err != nil {
    return err
}

switch storageInfo.VFSType {
case wasmsqlite.VFSTypeOPFS:
    // Persistent OPFS storage.
case wasmsqlite.VFSTypeOPFSWebLocks:
    // Persistent OPFS storage using SQLite's Web Locks VFS.
case wasmsqlite.VFSTypeOPFSSAHPool:
    // Persistent OPFS storage using SQLite's SAH pool VFS.
case wasmsqlite.VFSTypeMemory:
    // In-memory storage; data will reset on refresh.
}
```

`GetVFSType(conn)` remains available when code already has a dedicated `*sql.Conn`. Use `DisallowMemory: true`, `RequirePersistent: true`, `disallow_memory=true`, or `require_persistent=true` when falling back to memory would be incorrect.

## Storage Behavior

- One `*sql.DB` should own a given OPFS filename in a page. Use `SetMaxOpenConns(1)`.
- Opening the same OPFS filename twice in the same Worker returns `ErrDuplicateOpen`.
- Opening the same OPFS filename from two tabs uses separate Workers and browser OPFS locking. Browser behavior can differ; apps should handle either a successful second open or an actionable lock/open error.
- Default `vfs=auto` tries `opfs-wl`, then `opfs-sahpool`, then `opfs`, then `memory` if fallback is allowed.
- Use `vfs=opfs-wl` for multi-tab workloads where Web Locks support is available. It preserves OPFS persistence while letting the browser arbitrate lock acquisition more fairly across tabs.
- Use `vfs=opfs-sahpool` for the explicit SQLite SAH pool VFS when single-tab performance is more important than transparent multi-tab access.
- Private/incognito modes may expose reduced or temporary OPFS storage. Use `DisallowMemory` or `RequirePersistent` when data loss would be unacceptable.

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
make test           # Run normal Go tests
make browser-test   # Run headless Chrome browser E2E tests
npm test            # Run Playwright tests against the built example page
make clean          # Remove build artifacts
```

`make fetch-assets` reads the official SQLite download metadata and fetches the current `sqlite-wasm-*.zip` bundle with SHA3 verification. To pin a specific archive, pass `SQLITE_URL` and `EXPECTED_SHA`; `SQLITE_VERSION=3530100 make fetch-assets` selects that version when it is still listed on the download page.

Browser tests build a WASM test binary, serve the SQLite assets with the required headers, launch headless Chrome, and verify OPFS persistence, BLOBs, dump/load, transactions, memory mode, migrations, generated SQL shapes, cancellation, named parameters, browser-context storage behavior, and the static Pages-style example path. CI runs these browser tests on every push and pull request.

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
- If OPFS is unavailable, `vfs=auto` and `vfs=opfs` fall back to in-memory storage unless memory fallback is disabled.
- Context cancellation stops waiting on the Go side but does not interrupt already-running SQLite work in the Worker.
- Named and positional parameters cannot be mixed in the same call.

## Troubleshooting

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| `ErrBridgeNotLoaded` | `sqlite-bridge.js` was not loaded before `main.wasm` | Load `sqlite-bridge.js` before starting Go WASM. |
| `ErrAssetUnavailable` | Worker cannot fetch `sqlite3.js`, `sqlite3.wasm`, or the OPFS proxy | Serve the flat runtime files together or set `worker_url` and `sqlite_js_url`. |
| `ErrPersistentRequired` | `RequirePersistent` or `DisallowMemory` was set but OPFS/persistent storage is unavailable | Show a user-facing persistence error or allow memory fallback for demo mode. |
| `ErrDuplicateOpen` | Same OPFS filename opened twice in one Worker | Use one `*sql.DB` per OPFS file and `SetMaxOpenConns(1)`. |
| `ErrUnsupportedVFS` | Requested VFS is not available in the browser build | Use `auto`, `opfs`, `opfs-wl`, `opfs-sahpool`, or `memory`. |
| `ErrNamedParameter` | Mixed positional/named params, missing named param, or unused named param | Use all positional or all named params and match SQL names. |
| `ErrProtocolMismatch` | Cached runtime JS files are from different versions | Redeploy `sqlite-bridge.js`, `sqlite-worker.js`, SQLite assets, and `main.wasm` together. |

## Compatibility

| Component | Current support |
| --- | --- |
| Go | 1.24+ |
| SQLite WASM | Official `sqlite-wasm` bundle, currently 3.53.1 / 3530100 |
| Browser | Modern Chromium-class browsers with WebAssembly; OPFS requires secure context and cross-origin isolation |
| VFS modes | `auto`, `opfs-wl`, `opfs-sahpool`, `opfs`, `memory` |
| SQL API | Go `database/sql`, positional and named parameters, transactions, BLOBs, `parse_time`, dump/load |
| Unsupported | Dynamic SQLite extensions, forced interruption of already-running Worker SQL |

Releases should follow semantic versioning. Runtime JS/WASM files are part of the compatibility contract and should be deployed with the matching Go module version.

## License

MIT

## Acknowledgments

- [SQLite](https://sqlite.org/) for SQLite and the official WASM build
- [database/sql](https://pkg.go.dev/database/sql) for the standard Go SQL API
