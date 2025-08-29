Absolutely—here’s an updated, concise implementation plan that includes the **TypeScript → Worker JS** build, while still shipping a **Go-only runtime** for consumers.

# 1) Repo layout

```
wasmsqlite/
  assets/                    # built artifacts committed for consumers
    bridge.worker.js
    sqlite3.wasm
  worker/                    # TS source (for maintainers)
    src/bridge.worker.ts
    package.json
    tsconfig.json
    esbuild.mjs              # or rollup.config.mjs
  assets_embed.go            //go:embed assets/*
  driver.go                  // database/sql driver (syscall/js)
  open.go                    // worker bootstrap, DSN/options
  rows.go, tx.go, protocol.go
  internal/queue.go
  example/
  README.md
```

# 2) Build approach (maintainers only)

* Use **esbuild** (or Rollup) to bundle TS → a **single classic Worker file** (`bridge.worker.js`).
* **Pin** `@sqlite.org/sqlite-wasm` in `worker/package.json`.
* Configure the bundle so the Worker **does not fetch** the wasm:

  * The Go side sends `sqlite3.wasm` bytes via `postMessage` (transferable).
  * The Worker passes those bytes to the init function as `wasmBinary`.
* Release process runs `npm ci && npm run build` and **commits the built JS** + the pinned `sqlite3.wasm` into `assets/`. Consumers don’t need Node.

**worker/package.json (sketch)**

```json
{
  "type": "module",
  "private": true,
  "dependencies": {
    "@sqlite.org/sqlite-wasm": "3.46.0-buildX"  // pin exact
  },
  "devDependencies": { "esbuild": "^0.23.0" },
  "scripts": { "build": "node esbuild.mjs" }
}
```

**worker/esbuild.mjs (sketch)**

```js
import esbuild from 'esbuild';
await esbuild.build({
  entryPoints: ['worker/src/bridge.worker.ts'],
  outfile: 'assets/bridge.worker.js',
  bundle: true,
  format: 'iife',          // classic Worker
  platform: 'browser',
  target: ['es2020'],
  minify: true,
});
```

# 3) Worker TS (generic, schema-agnostic)

```ts
// worker/src/bridge.worker.ts
import sqlite3InitModule from '@sqlite.org/sqlite-wasm';

type Req =
  | { id:number; type:'loadWASM'; wasm:ArrayBuffer }
  | { id:number; type:'open'; file:string; vfs?:string }
  | { id:number; type:'exec'; sql:string; params?:any[] }
  | { id:number; type:'query'; sql:string; params?:any[] }
  | { id:number; type:'begin'|'commit'|'rollback'|'close' };

let sqlite3: any, db: any;

self.onmessage = async (e: MessageEvent<Req>) => {
  const { id, type } = e.data;
  const ok = (payload:object = {}) => (postMessage({ id, ok:true, ...payload }));
  const fail = (err:any) => postMessage({ id, ok:false, error:String(err) });
  try {
    if (type === 'loadWASM') {
      sqlite3 = await sqlite3InitModule({ wasmBinary: e.data.wasm });
      ok();
    } else if (type === 'open') {
      db = new sqlite3.oo1.DB({ filename: e.data.file, flags: 'ct', vfs: e.data.vfs ?? 'opfs-sahpool' });
      ok();
    } else if (type === 'exec') {
      db.exec(e.data.sql, { bind: e.data.params ?? [] });
      ok({ rowsAffected: db.changes(), lastInsertId: db.lastInsertRowid() });
    } else if (type === 'query') {
      const rows = db.selectArrays(e.data.sql, e.data.params ?? []);
      // Column names: run a PRAGMA or infer via prepared stmt if you want; optional for MVP
      ok({ rows });
    } else if (type === 'begin')   { db.exec('BEGIN IMMEDIATE'); ok(); }
      else if (type === 'commit')  { db.exec('COMMIT'); ok(); }
      else if (type === 'rollback'){ db.exec('ROLLBACK'); ok(); }
      else if (type === 'close')   { db?.close(); db = null; ok(); }
  } catch (err) { fail(err); }
};
```

# 4) Go side: boot + embed (unchanged for consumers)

* `assets_embed.go` embeds `bridge.worker.js` and `sqlite3.wasm`.
* On `Open`, create a **Blob URL** for the Worker JS, `new Worker(url)`, then:

  1. `postMessage({type:'loadWASM', wasm:<ArrayBuffer>}, [arrayBuffer])`
  2. `postMessage({type:'open', file:'/app.db', vfs:'opfs-sahpool'})`
* Implement `ExecContext`, `QueryContext`, `BeginTx`, `Tx.Commit/Rollback`, and `Rows` to map to the Worker protocol.

# 5) DSN / Options

* `file=/app.db`, `vfs=opfs-sahpool` (default), `busy_timeout=5000`, `worker_url=` (optional override), `parse_time=true` (optional).

# 6) CI & release

* CI job:

  1. `npm ci && npm run build` in `/worker`
  2. verify `assets/bridge.worker.js` + `assets/sqlite3.wasm` updated
  3. `go vet && go test` (use `wasmbrowsertest`)
  4. tag release; include SQLite version in release notes
* Consumers: **no Node** required; just `go get` and `sql.Open("wasmsqlite", ...)`.

# 7) Optional “bundler path” for web apps

* Publish the TS Worker **as well** (`worker/src/bridge.worker.ts`) so apps with Vite/Webpack can self-bundle.
* Expose `worker_url` DSN to point at their hosted worker; skip `//go:embed` path in that case.

---

**Outcome:** You maintain TypeScript source and a reproducible build to a single `bridge.worker.js`. You ship the built JS + pinned `sqlite3.wasm` embedded in your Go module so any sqlc project can run in the browser with **no JS toolchain**.

Here’s a tight, step-by-step plan to ship the **Go runtime module** (generic for any sqlc SQLite project).

# 1) Scope & deliverables

* **Goal:** Provide a `database/sql` driver for the browser that talks to SQLite WASM on OPFS via a Web Worker.
* **Deliverables:**

  * Go module `wasmsqlite` (driver + helpers).
  * Prebuilt `bridge.worker.js` and `sqlite3.wasm`, embedded via `//go:embed`.
  * Minimal demo showing sqlc-generated code working unchanged.

# 2) Repo layout

```
wasmsqlite/
  assets/
    bridge.worker.js
    sqlite3.wasm
  assets_embed.go     //go:embed worker+wasm
  driver.go           database/sql driver (syscall/js)
  rows.go             driver.Rows implementation
  tx.go               driver.Tx implementation
  open.go             Open/Options, DSN parser, worker bootstrap
  protocol.go         request/response structs, type mapping utils
  internal/queue.go   request queue & ctx cancellation wiring
  example/            tiny demo wiring sqlc Queries
  README.md
```

# 3) Public API

* **Side-effect registration:**

  ```go
  import _ "example.com/wasmsqlite"
  db, _ := sql.Open("wasmsqlite", "file=/app.db?vfs=opfs-sahpool&busy_timeout=5000")
  ```
* **Helper:**

  ```go
  db, _ := wasmsqlite.Open(wasmsqlite.Options{
    File: "/app.db", VFS: "opfs-sahpool", BusyTimeout: 5000,
  })
  ```
* **Options/DSN keys:** `file`, `vfs`, `busy_timeout`, `journal_mode`, `pragma`, `worker_url` (override embedded worker), `read_only` (optional).

# 4) Worker protocol (generic, schema-agnostic)

* Request envelope: `{ id: number, type: "loadWASM"|"open"|"exec"|"query"|"begin"|"commit"|"rollback"|"close", payload, signal? }`
* Responses: `{ id, ok: boolean, error?: string, columns?: string[], rows?: any[][], rowsAffected?: number, lastInsertId?: number }`
* Semantics:

  * `loadWASM { wasm:ArrayBuffer }` → initialize sqlite module (no network).
  * `open { file, vfs }` → create single connection.
  * `exec { sql, params: any[] }`
  * `query { sql, params: any[] }` → returns `columns`, `rows`.
  * `begin/commit/rollback` serialize inside the worker.
  * Optional `signal` hooks to support cancellation.

# 5) Driver internals (Go)

* **Implement**: `driver.Driver(…Context)`, `driver.Conn` (+ `ExecerContext`, `QueryerContext`, `ConnBeginTx`, `Pinger`), `driver.Tx`, `driver.Rows`.
* **Lifecycle:**

  1. On `Open`, create Worker from embedded JS (Blob URL), send `loadWASM` with embedded bytes, then `open`.
  2. Maintain a **single connection**; serialize all requests with a queue.
  3. On `Close`, send `close`, terminate Worker, revoke Blob URL.
* **Context/cancellation:** map `ctx.Done()` → abort message (mark request cancelled; Worker can ignore result if late).
* **Type mapping:** JS → `driver.Value`

  * number → `float64`, try int-fit for whole numbers (document behavior)
  * string → `string`
  * `Uint8Array` → `[]byte`
  * `null` → `nil`
  * (Optional) RFC3339 text → `time.Time` if caller opts in via DSN (`parse_time=true`)
* **Transactions:** send `BEGIN IMMEDIATE`/`COMMIT`/`ROLLBACK`; track active txn to gate concurrent ops.

# 6) Asset embedding & bootstrap

* `//go:embed assets/bridge.worker.js` → `workerJS []byte`
* `//go:embed assets/sqlite3.wasm` → `sqliteWASM []byte`
* Create Worker via Blob: `new Worker(URL.createObjectURL(new Blob([workerJS])))`
* Transfer WASM: post `sqliteWASM`’s `ArrayBuffer` with transferable semantics.

# 7) Concurrency rules

* One connection per `*sql.DB` (and per origin file). Queue all ops.
* While a transaction is open, only allow ops within that txn; block others.

# 8) Error handling

* Normalize Worker errors into Go `error` with SQLite code if available.
* Timeouts: if `ctx` deadline exceeded, return `context.DeadlineExceeded`.
* On Worker crash: mark conn bad so `database/sql` can reopen (document limitations).

# 9) Build & run (docs + example)

* **Build:** `GOOS=js GOARCH=wasm go build -o web/app.wasm ./cmd/demo`
* **Serve:** `index.html` loads `wasm_exec.js` + `app.wasm` over HTTPS/localhost.
* **Demo:** run a few `INSERT/SELECT` via sqlc `Queries`.

# 10) Testing plan

* **Unit (headless):** run tiny harness with `wasmbrowsertest` (Chrome/Firefox). Cover:

  * open/close, exec/query, transactions, cancellation, blobs, large result sets.
* **Integration:** use a generated sqlc package (schema + queries) and assert results match a native SQLite run for the same dataset.
* **Cross-browser:** Chrome, Edge, Firefox; Safari TP if OPFS changes matter.
* **CSP:** test with `worker-src blob:` and with hosted `worker_url` fallback.

# 11) Performance checklist

* Use `opfs-sahpool` by default (DSN override).
* Batch param binding; avoid per-row postMessage overhead (return all rows at once; paginate if very large).
* Reuse prepared statements internally (optional optimization phase 2).
* Expose `busy_timeout` (default 5s).

# 12) Compatibility & fallbacks

* If OPFS unavailable: return clear error or allow `:memory:` fallback when `allow_memory=true`.
* Document HTTPS requirement and private browsing caveats.

# 13) Versioning & release

* Pin SQLite WASM version; expose `wasmsqlite.Version()`.
* GitHub Releases with checksums; changelog on SQLite bumps.
* Semantic versioning; breaking changes only on major.

# 14) Security & supply chain

* No network fetch for WASM by default; all embedded.
* Optional `worker_url` for apps with strict CSP; document integrity strategy if they self-host.

# 15) Future enhancements (post-MVP)

* Prepared statement cache (name→SQL) to reduce parse overhead.
* Type-hints map (opt-in) to coerce cols more precisely (e.g., integers vs floats, RFC3339 to time).
* Streaming row iteration (chunked responses) for huge selects.
* WAL2/PRAGMA presets when browser support lands.

---

**Acceptance criteria (MVP):**

* `sqlc`-generated Go code runs unchanged in browser, using `*sql.DB` backed by this driver.
* CRUD + transactions work against a persistent OPFS file across reloads.
* Works in Chromium + Firefox with default settings.
* No npm/bundler required for consumers (prebuilt Worker + embedded WASM).
