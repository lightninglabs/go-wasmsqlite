Perfect — then you don’t need an sqlc plugin. Ship a **Go runtime module** that registers a `database/sql` driver and embeds the prebuilt Worker + SQLite WASM. Any sqlc SQLite project can then do `sql.Open("wasmsqlite", ...)` and keep its generated Go API unchanged.

Here’s the shape:

# 1) Module layout

```
wasmsqlite/
  assets/
    bridge.worker.js      # prebuilt, generic
    sqlite3.wasm          # pinned
  driver.go               # database/sql driver (syscall/js)
  assets_embed.go         # //go:embed the two files
  open.go                 # Open/Options helpers, DSN parser
  rows.go                 # driver.Rows impl
  tx.go                   # driver.Tx impl
  README.md
```

# 2) Public API (tiny)

```go
// import side-effect registers the driver
import _ "example.com/wasmsqlite"

db, err := sql.Open("wasmsqlite", "file=/app.db?vfs=opfs-sahpool")
// or:
db, err := wasmsqlite.Open(wasmsqlite.Options{
  File: "/app.db", VFS: "opfs-sahpool", BusyTimeout: 5000,
})
```

**Why this works:** sqlc’s `Queries` only needs a `database/sql`-compatible connection (it calls ExecContext/QueryContext/etc.). Your driver provides that, while the Worker runs SQLite on OPFS.

# 3) Core internals (what you implement once)

**Embed assets**

```go
// assets_embed.go
package wasmsqlite

import _ "embed"

//go:embed assets/bridge.worker.js
var workerJS []byte

//go:embed assets/sqlite3.wasm
var sqliteWASM []byte
```

**Spin up the Worker (no bundler required)**

```go
// open.go
func newWorker() (js.Value, error) {
  u8 := js.Global().Get("Uint8Array").New(len(workerJS))
  js.CopyBytesToJS(u8, workerJS)
  blob := js.Global().Get("Blob").New([]any{u8}, map[string]any{"type":"text/javascript"})
  url  := js.Global().Get("URL").Call("createObjectURL", blob)
  return js.Global().Get("Worker").New(url), nil
}
```

**Load WASM and open DB**

* Send one message with a **transferable** `ArrayBuffer` of `sqlite3.wasm`.
* Then send `open { file: "/app.db", vfs: "opfs-sahpool" }`.

**Message protocol (generic)**

* `loadWASM { wasm:ArrayBuffer }`
* `open { file, vfs }`
* `exec { sql, params } -> { rowsAffected, lastInsertId }`
* `query { sql, params } -> { columns, rows }`
* `begin/commit/rollback`
* `close`

**Driver interfaces to implement**

* `driver.Driver` (or `DriverContext`)
* `driver.Conn` (+ `ExecerContext`, `QueryerContext`, `ConnBeginTx`, `Pinger`)
* `driver.Tx`
* `driver.Rows` (wrap the returned `columns` + `rows`)

Keep one connection per DB file and **serialize requests** (simple queue) — it’s the safest default for OPFS.

# 4) Generic Worker (prebuilt JS)

Your `bridge.worker.js` is schema-agnostic. Pseudocode:

```js
let sqlite3, db, wasmReady;
self.onmessage = async (e) => {
  const { id, type, ...p } = e.data;
  try {
    if (type === "loadWASM") {
      const mod = await WebAssembly.compile(p.wasm);
      // init sqlite3 module with precompiled bytes (implementation detail of chosen wrapper)
      sqlite3 = await initSqliteFromModule(mod); // your wrapper helper
      wasmReady = true;
      postMessage({ id, ok: true });
    } else if (type === "open") {
      ensure(wasmReady);
      db = new sqlite3.oo1.DB({ filename: p.file, flags: "ct", vfs: p.vfs || "opfs-sahpool" });
      postMessage({ id, ok: true });
    } else if (type === "exec") {
      db.exec(p.sql, { bind: p.params || [] });
      postMessage({ id, ok: true, rowsAffected: db.changes(), lastInsertId: db.lastInsertRowid() });
    } else if (type === "query") {
      const rows = db.selectArrays(p.sql, p.params || []);
      const cols = db.columnNames(p.sql); // or infer from first row; you choose
      postMessage({ id, ok: true, columns: cols, rows });
    } else if (type === "begin")   { db.exec("BEGIN IMMEDIATE"); postMessage({ id, ok: true }); }
      else if (type === "commit")  { db.exec("COMMIT");          postMessage({ id, ok: true }); }
      else if (type === "rollback"){ db.exec("ROLLBACK");        postMessage({ id, ok: true }); }
    else if (type === "close")     { db?.close(); db=null; postMessage({ id, ok: true }); }
  } catch (err) { postMessage({ id, ok: false, error: String(err) }); }
};
```

(You’ll use the official SQLite WASM “oo1” API under the hood; above is the shape.)

# 5) Build & run

* Build your app for the browser:

  ```
  GOOS=js GOARCH=wasm go build -o web/app.wasm ./cmd/app
  ```
* Serve an `index.html` that loads Go’s `wasm_exec.js` and your `app.wasm`.
* Run on HTTPS (or localhost) so OPFS is available.

# 6) Small but important knobs

* DSN / Options: `file` (default `/app.db`), `vfs` (`opfs-sahpool` default), `busy_timeout`, `pragma` (optional), `workerURL` override (if an app wants to host its own).
* Context cancellation: map `ctx.Done()` to an `AbortController` in the Worker and early-fail the request.
* Types: map JS → `driver.Value` (`float64`, `int64`, `string`, `[]byte`, `nil`). Parse RFC3339 text → `time.Time` when the sqlc column type hints say so (optional).
* Migrations (optional): add `wasmsqlite.Migrate(ctx, db, schemaSQL []byte)` that runs once.

---

If you want, I can sketch the exact `driver.go` skeleton (interfaces + request queue) and a minimal Worker that works with the official SQLite WASM “oo1” build; you’d just drop in your module path and it’ll run.
