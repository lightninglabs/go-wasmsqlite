//go:build js && wasm

package wasmsqlite

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"syscall/js"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratetesting "github.com/golang-migrate/migrate/v4/database/testing"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	database "github.com/lightninglabs/go-wasmsqlite/example/generated"
)

//go:embed example/migrations/*.sql
var browserMigrationFS embed.FS

func TestBrowserDriverOPFSE2E(t *testing.T) {
	filename := fmt.Sprintf("/browser-test-%d.db", time.Now().UnixNano())
	db, err := Open(&Options{File: filename, VFS: "opfs", BusyTimeout: 5000})
	if err != nil {
		t.Fatalf("open opfs db: %v", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	vfs, err := GetVFSType(conn)
	conn.Close()
	if err != nil {
		t.Fatalf("get vfs type: %v", err)
	}
	if vfs != VFSTypeOPFS {
		t.Fatalf("expected OPFS VFS, got %s", vfs)
	}

	exerciseDatabaseSQL(t, db)
	exerciseTransactions(t, db)
	exerciseDumpLoad(t, db)

	if err := db.Close(); err != nil {
		t.Fatalf("close opfs db: %v", err)
	}

	reopened, err := Open(&Options{File: filename, VFS: "opfs", BusyTimeout: 5000})
	if err != nil {
		t.Fatalf("reopen opfs db: %v", err)
	}
	defer reopened.Close()

	var count int
	if err := reopened.QueryRow(`SELECT COUNT(*) FROM typed_values WHERE label = ?`, "first").Scan(&count); err != nil {
		t.Fatalf("query reopened db: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected persisted row after reopen, got count=%d", count)
	}
}

func TestBrowserDriverAutoVFSE2E(t *testing.T) {
	filename := fmt.Sprintf("/browser-auto-%d.db", time.Now().UnixNano())
	db, err := Open(&Options{File: filename, VFS: "auto", DisallowMemory: true, BusyTimeout: 5000})
	if err != nil {
		t.Fatalf("open auto db handle: %v", err)
	}
	defer db.Close()

	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping auto db: %v", err)
	}

	info, err := GetStorageInfo(db)
	if err != nil {
		t.Fatalf("get auto storage info: %v", err)
	}
	if info.RequestedVFS != VFSTypeAuto {
		t.Fatalf("expected requested auto VFS, got %s", info.RequestedVFS)
	}
	if !info.OPFS || !info.Persistent || info.Memory {
		t.Fatalf("expected persistent OPFS storage info, got %+v", info)
	}
	switch info.VFSType {
	case VFSTypeOPFSWebLocks, VFSTypeOPFSSAHPool, VFSTypeOPFS:
	default:
		t.Fatalf("expected auto to resolve to OPFS VFS, got %+v", info)
	}

	if _, err := db.Exec(`CREATE TABLE auto_values (id INTEGER PRIMARY KEY, label TEXT); INSERT INTO auto_values(label) VALUES ('ok');`); err != nil {
		t.Fatalf("exec auto SQL: %v", err)
	}
	var label string
	if err := db.QueryRow(`SELECT label FROM auto_values WHERE id = 1`).Scan(&label); err != nil {
		t.Fatalf("query auto row: %v", err)
	}
	if label != "ok" {
		t.Fatalf("unexpected auto label: %q", label)
	}
}

func TestBrowserDriverMemoryE2E(t *testing.T) {
	db, err := Open(&Options{File: ":memory:", VFS: "memory"})
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE mem_values (id INTEGER PRIMARY KEY, label TEXT)`); err != nil {
		t.Fatalf("create memory schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO mem_values(label) VALUES (?)`, "ok"); err != nil {
		t.Fatalf("insert memory row: %v", err)
	}
	var label string
	if err := db.QueryRow(`SELECT label FROM mem_values WHERE id = 1`).Scan(&label); err != nil {
		t.Fatalf("query memory row: %v", err)
	}
	if label != "ok" {
		t.Fatalf("unexpected memory label: %q", label)
	}
}

func TestBrowserContextCancellationE2E(t *testing.T) {
	db, err := Open(&Options{File: ":memory:", VFS: "memory"})
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer db.Close()

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = conn.Raw(func(driverConn interface{}) error {
		c, ok := driverConn.(*Conn)
		if !ok {
			t.Fatalf("unexpected driver connection type %T", driverConn)
		}
		_, err := c.QueryContext(ctx, `SELECT 1`, nil)
		return err
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestBrowserNamedParametersE2E(t *testing.T) {
	db, err := Open(&Options{File: ":memory:", VFS: "memory"})
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE named_values (id INTEGER PRIMARY KEY, label TEXT, score INTEGER, data BLOB)`); err != nil {
		t.Fatalf("create named schema: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO named_values(id, label, score, data) VALUES (:id, $label, @score, :data)`,
		sql.Named("id", 7),
		sql.Named("label", "named"),
		sql.Named("score", int64(99)),
		sql.Named("data", []byte{0x01, 0x02}),
	); err != nil {
		t.Fatalf("insert named params: %v", err)
	}

	var (
		label string
		score int64
		data  []byte
	)
	if err := db.QueryRow(
		`SELECT label, score, data FROM named_values WHERE id = :id`,
		sql.Named("id", 7),
	).Scan(&label, &score, &data); err != nil {
		t.Fatalf("query named params: %v", err)
	}
	if label != "named" || score != 99 || !bytes.Equal(data, []byte{0x01, 0x02}) {
		t.Fatalf("unexpected named values: label=%q score=%d data=%x", label, score, data)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()

	err = conn.Raw(func(driverConn interface{}) error {
		c, ok := driverConn.(*Conn)
		if !ok {
			t.Fatalf("unexpected driver connection type %T", driverConn)
		}
		_, err := c.QueryContext(context.Background(), `SELECT :id, ?`, []driver.NamedValue{
			{Ordinal: 1, Name: "id", Value: 1},
			{Ordinal: 2, Value: 2},
		})
		return err
	})
	if !errors.Is(err, ErrNamedParameter) {
		t.Fatalf("expected ErrNamedParameter for mixed params, got %v", err)
	}
}

func TestBrowserExecBatchE2E(t *testing.T) {
	db, err := Open(&Options{File: ":memory:", VFS: "memory"})
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE batch_values (
		id INTEGER PRIMARY KEY,
		label TEXT NOT NULL,
		payload BLOB
	)`); err != nil {
		t.Fatalf("create batch schema: %v", err)
	}

	result, err := ExecBatchContext(context.Background(), db,
		`INSERT INTO batch_values(id, label, payload) VALUES (?, ?, ?)`,
		[][]any{
			{int64(1), "first", []byte{0x01, 0x02}},
			{int64(2), "second", nil},
			{int64(3), "third", []byte{}},
		},
	)
	if err != nil {
		t.Fatalf("exec batch insert: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("batch rows affected: %v", err)
	}
	if rowsAffected != 3 {
		t.Fatalf("expected 3 rows affected, got %d", rowsAffected)
	}

	lastInsertID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("batch last insert id: %v", err)
	}
	if lastInsertID != 3 {
		t.Fatalf("expected last insert id 3, got %d", lastInsertID)
	}

	var (
		count  int
		label  string
		data   []byte
		isNull bool
	)
	if err := db.QueryRow(`SELECT COUNT(*) FROM batch_values`).Scan(&count); err != nil {
		t.Fatalf("count batch rows: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 batch rows, got %d", count)
	}
	if err := db.QueryRow(`SELECT label, payload FROM batch_values WHERE id = 1`).Scan(&label, &data); err != nil {
		t.Fatalf("query first batch row: %v", err)
	}
	if label != "first" || !bytes.Equal(data, []byte{0x01, 0x02}) {
		t.Fatalf("unexpected first batch row: label=%q data=%x", label, data)
	}
	if err := db.QueryRow(`SELECT payload IS NULL FROM batch_values WHERE id = 2`).Scan(&isNull); err != nil {
		t.Fatalf("query nil batch payload: %v", err)
	}
	if !isNull {
		t.Fatal("nil batch payload should bind as NULL")
	}
	if err := db.QueryRow(`SELECT payload FROM batch_values WHERE id = 3`).Scan(&data); err != nil {
		t.Fatalf("query empty batch payload: %v", err)
	}
	if data == nil || len(data) != 0 {
		t.Fatalf("empty batch payload mismatch: %v", data)
	}

	emptyResult, err := ExecBatchContext(context.Background(), db,
		`INSERT INTO batch_values(id, label) VALUES (?, ?)`,
		nil,
	)
	if err != nil {
		t.Fatalf("exec empty batch: %v", err)
	}
	rowsAffected, err = emptyResult.RowsAffected()
	if err != nil {
		t.Fatalf("empty batch rows affected: %v", err)
	}
	if rowsAffected != 0 {
		t.Fatalf("expected empty batch to affect 0 rows, got %d", rowsAffected)
	}
}

func TestBrowserExecBatchRollbackOnErrorE2E(t *testing.T) {
	db, err := Open(&Options{File: ":memory:", VFS: "memory"})
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE batch_unique (
		id INTEGER PRIMARY KEY,
		label TEXT NOT NULL UNIQUE
	)`); err != nil {
		t.Fatalf("create unique batch schema: %v", err)
	}

	_, err = ExecBatchContext(context.Background(), db,
		`INSERT INTO batch_unique(id, label) VALUES (?, ?)`,
		[][]any{
			{int64(10), "dup"},
			{int64(11), "dup"},
		},
	)
	if err == nil {
		t.Fatal("expected duplicate batch insert to fail")
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM batch_unique`).Scan(&count); err != nil {
		t.Fatalf("count rows after failed batch: %v", err)
	}
	if count != 0 {
		t.Fatalf("failed batch should have rolled back all rows, got count=%d", count)
	}

	if _, err := ExecBatchContext(context.Background(), db,
		`INSERT INTO batch_unique(id, label) VALUES (?, ?)`,
		[][]any{{int64(12), "ok"}},
	); err != nil {
		t.Fatalf("exec batch after rollback: %v", err)
	}
}

func TestBrowserExecBatchLargeE2E(t *testing.T) {
	db, err := Open(&Options{File: ":memory:", VFS: "memory"})
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE batch_large (
		id INTEGER PRIMARY KEY,
		label TEXT NOT NULL,
		score INTEGER NOT NULL
	)`); err != nil {
		t.Fatalf("create large batch schema: %v", err)
	}

	const batchSize = 10_000
	rows := make([][]any, batchSize)
	var wantSum int64
	for i := 0; i < batchSize; i++ {
		id := int64(i + 1)
		score := id * 3
		rows[i] = []any{id, fmt.Sprintf("label-%05d", id), score}
		wantSum += score
	}

	start := time.Now()
	result, err := ExecBatchContext(context.Background(), db,
		`INSERT INTO batch_large(id, label, score) VALUES (?, ?, ?)`,
		rows,
	)
	if err != nil {
		t.Fatalf("exec large batch insert: %v", err)
	}
	t.Logf("inserted %d rows with ExecBatch in %s", batchSize, time.Since(start))

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("large batch rows affected: %v", err)
	}
	if rowsAffected != batchSize {
		t.Fatalf("expected %d rows affected, got %d", batchSize, rowsAffected)
	}

	lastInsertID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("large batch last insert id: %v", err)
	}
	if lastInsertID != batchSize {
		t.Fatalf("expected last insert id %d, got %d", batchSize, lastInsertID)
	}

	var (
		count    int
		sumScore int64
		first    string
		last     string
	)
	if err := db.QueryRow(`
SELECT COUNT(*), SUM(score), MIN(label), MAX(label)
FROM batch_large
`).Scan(&count, &sumScore, &first, &last); err != nil {
		t.Fatalf("query large batch aggregate: %v", err)
	}
	if count != batchSize || sumScore != wantSum ||
		first != "label-00001" || last != "label-10000" {
		t.Fatalf(
			"large batch mismatch: count=%d sum=%d first=%q last=%q",
			count, sumScore, first, last,
		)
	}
}

func TestBrowserExecBatchHugeBlobE2E(t *testing.T) {
	if os.Getenv("WASMSQLITE_HUGE_BATCH_TEST") != "1" {
		t.Skip("set WASMSQLITE_HUGE_BATCH_TEST=1 to run huge batch stress test")
	}

	db, err := Open(&Options{File: ":memory:", VFS: "memory"})
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE batch_huge_blob (
		id INTEGER PRIMARY KEY,
		b0 BLOB NOT NULL,
		b1 BLOB NOT NULL,
		b2 BLOB NOT NULL,
		b3 BLOB NOT NULL,
		b4 BLOB NOT NULL,
		b5 BLOB NOT NULL,
		b6 BLOB NOT NULL,
		b7 BLOB NOT NULL,
		b8 BLOB NOT NULL,
		b9 BLOB NOT NULL
	)`); err != nil {
		t.Fatalf("create huge blob batch schema: %v", err)
	}

	const (
		batchSize  = 100_000
		blobSize   = 256
		blobCols   = 10
		rawPayload = batchSize * blobCols * blobSize
	)

	blob := make([]byte, blobSize)
	for i := range blob {
		blob[i] = byte((i * 31) % 251)
	}

	rows := make([][]any, batchSize)
	for i := 0; i < batchSize; i++ {
		id := int64(i + 1)
		rows[i] = []any{
			id,
			blob,
			blob,
			blob,
			blob,
			blob,
			blob,
			blob,
			blob,
			blob,
			blob,
		}
	}

	start := time.Now()
	result, err := ExecBatchContext(context.Background(), db, `
INSERT INTO batch_huge_blob (
	id, b0, b1, b2, b3, b4, b5, b6, b7, b8, b9
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, rows)
	if err != nil {
		t.Fatalf("exec huge blob batch insert (%d raw bytes): %v", rawPayload, err)
	}
	js.Global().Get("console").Call(
		"log",
		fmt.Sprintf(
			"huge ExecBatch inserted %d rows x %d blob columns x %d bytes (%d raw bytes) in %s",
			batchSize, blobCols, blobSize, rawPayload, time.Since(start),
		),
	)
	t.Logf(
		"inserted %d rows x %d blob columns x %d bytes (%d raw bytes) in %s",
		batchSize, blobCols, blobSize, rawPayload, time.Since(start),
	)

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("huge blob batch rows affected: %v", err)
	}
	if rowsAffected != batchSize {
		t.Fatalf("expected %d rows affected, got %d", batchSize, rowsAffected)
	}

	lastInsertID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("huge blob batch last insert id: %v", err)
	}
	if lastInsertID != batchSize {
		t.Fatalf("expected last insert id %d, got %d", batchSize, lastInsertID)
	}

	var (
		count        int
		totalB0Bytes int64
		totalB9Bytes int64
		firstB0      []byte
		lastB9       []byte
	)
	if err := db.QueryRow(`
SELECT COUNT(*), SUM(length(b0)), SUM(length(b9)),
       (SELECT b0 FROM batch_huge_blob WHERE id = 1),
       (SELECT b9 FROM batch_huge_blob WHERE id = 100000)
FROM batch_huge_blob
`).Scan(&count, &totalB0Bytes, &totalB9Bytes, &firstB0, &lastB9); err != nil {
		t.Fatalf("query huge blob batch aggregate: %v", err)
	}
	if count != batchSize ||
		totalB0Bytes != int64(batchSize*blobSize) ||
		totalB9Bytes != int64(batchSize*blobSize) ||
		!bytes.Equal(firstB0, blob) ||
		!bytes.Equal(lastB9, blob) {
		t.Fatalf(
			"huge blob batch mismatch: count=%d b0bytes=%d b9bytes=%d first=%d last=%d",
			count, totalB0Bytes, totalB9Bytes, len(firstB0), len(lastB9),
		)
	}
}

func TestBrowserUpdateReturningWithNumberedParamsAndSubqueryE2E(t *testing.T) {
	db, err := Open(&Options{File: ":memory:", VFS: "memory"})
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
CREATE TABLE mailbox_messages (
	id INTEGER PRIMARY KEY,
	mailbox_id TEXT NOT NULL,
	correlation_key TEXT,
	lease_token TEXT,
	lease_until TEXT,
	attempts INTEGER NOT NULL DEFAULT 0,
	max_attempts INTEGER NOT NULL,
	available_at TEXT NOT NULL,
	created_at TEXT NOT NULL,
	priority INTEGER NOT NULL
);
`); err != nil {
		t.Fatalf("create mailbox_messages schema: %v", err)
	}

	seedRows := []struct {
		id             int
		mailboxID      string
		correlationKey sql.NullString
		attempts       int
		maxAttempts    int
		availableAt    string
		createdAt      string
		priority       int
	}{
		{
			id:             1,
			mailboxID:      "mailbox-a",
			correlationKey: sql.NullString{String: "group-a", Valid: true},
			attempts:       0,
			maxAttempts:    3,
			availableAt:    "2026-05-19T08:00:00Z",
			createdAt:      "2026-05-19T08:00:00Z",
			priority:       5,
		},
		{
			id:             2,
			mailboxID:      "mailbox-a",
			correlationKey: sql.NullString{String: "group-a", Valid: true},
			attempts:       0,
			maxAttempts:    3,
			availableAt:    "2026-05-19T08:01:00Z",
			createdAt:      "2026-05-19T08:01:00Z",
			priority:       10,
		},
		{
			id:             3,
			mailboxID:      "mailbox-a",
			correlationKey: sql.NullString{},
			attempts:       0,
			maxAttempts:    3,
			availableAt:    "2026-05-19T08:02:00Z",
			createdAt:      "2026-05-19T08:02:00Z",
			priority:       1,
		},
		{
			id:             4,
			mailboxID:      "mailbox-b",
			correlationKey: sql.NullString{},
			attempts:       0,
			maxAttempts:    3,
			availableAt:    "2026-05-19T08:00:00Z",
			createdAt:      "2026-05-19T08:00:00Z",
			priority:       100,
		},
	}
	for _, row := range seedRows {
		if _, err := db.Exec(`
INSERT INTO mailbox_messages (
	id, mailbox_id, correlation_key, attempts, max_attempts,
	available_at, created_at, priority
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			row.id,
			row.mailboxID,
			row.correlationKey,
			row.attempts,
			row.maxAttempts,
			row.availableAt,
			row.createdAt,
			row.priority,
		); err != nil {
			t.Fatalf("insert mailbox row %d: %v", row.id, err)
		}
	}

	const claimNextMessage = `
UPDATE mailbox_messages
SET
    lease_token = $2,
    lease_until = $3,
    attempts = attempts + 1
WHERE mailbox_messages.id = (
    SELECT m.id FROM mailbox_messages m
    WHERE m.mailbox_id = $1
      AND m.available_at <= $4
      AND (m.lease_until IS NULL OR m.lease_until < $4)
      AND m.attempts < m.max_attempts
      AND (
          m.correlation_key IS NULL
          OR NOT EXISTS (
              SELECT 1 FROM mailbox_messages m2
              WHERE m2.mailbox_id = m.mailbox_id
                AND m2.correlation_key = m.correlation_key
                AND m2.id < m.id
                AND m2.attempts < m2.max_attempts
          )
      )
    ORDER BY m.priority DESC, m.available_at ASC, m.created_at ASC
    LIMIT 1
)
RETURNING id, mailbox_id, lease_token, lease_until, attempts;`

	var (
		id         int
		mailboxID  string
		leaseToken string
		leaseUntil string
		attempts   int
	)
	err = db.QueryRow(
		claimNextMessage,
		"mailbox-a",
		"lease-1",
		"2026-05-19T09:00:00Z",
		"2026-05-19T08:30:00Z",
	).Scan(&id, &mailboxID, &leaseToken, &leaseUntil, &attempts)
	if err != nil {
		t.Fatalf("claim next mailbox message: %v", err)
	}

	if id != 1 || mailboxID != "mailbox-a" || leaseToken != "lease-1" ||
		leaseUntil != "2026-05-19T09:00:00Z" || attempts != 1 {
		t.Fatalf(
			"unexpected claimed row: id=%d mailbox=%q token=%q until=%q attempts=%d",
			id, mailboxID, leaseToken, leaseUntil, attempts,
		)
	}
}

// TestBrowserExecNumberedParamsOutOfOrderE2E exercises the database/sql exec
// path (ExecContext, no RETURNING) with positional $N placeholders whose
// numbers appear out of textual order: the SET clause references $3/$4 before
// the WHERE clause references $1/$2. The driver passes these as a positional
// array, which must be remapped to SQLite parameter slots BY NUMBER. A
// regression binds the WHERE keys against the SET values, so the statement
// matches no rows and silently updates nothing. This mirrors the sqlc :exec
// UPDATEs emitted for the PostgreSQL engine (e.g. UpdateVTXOStatus) that run
// against this driver in the browser. It complements
// TestBrowserUpdateReturningWithNumberedParamsAndSubqueryE2E, which only
// covers the query (RETURNING) path.
func TestBrowserExecNumberedParamsOutOfOrderE2E(t *testing.T) {
	db, err := Open(&Options{File: ":memory:", VFS: "memory"})
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
CREATE TABLE vtxos (
	outpoint_hash    TEXT NOT NULL,
	outpoint_index   INTEGER NOT NULL,
	status           INTEGER NOT NULL,
	last_update_time TEXT NOT NULL,
	PRIMARY KEY (outpoint_hash, outpoint_index)
);`); err != nil {
		t.Fatalf("create vtxos schema: %v", err)
	}

	if _, err := db.Exec(
		`INSERT INTO vtxos (outpoint_hash, outpoint_index, status, last_update_time)
		 VALUES (?, ?, ?, ?)`,
		"abcd", 0, 0, "2026-06-17T00:00:00Z",
	); err != nil {
		t.Fatalf("seed vtxo row: %v", err)
	}

	// SET references $3,$4 before WHERE references $1,$2 -- the exact shape that
	// mis-binds when a positional array is bound by appearance order instead of
	// by number.
	const updateStatus = `
UPDATE vtxos
SET status = $3,
    last_update_time = $4
WHERE outpoint_hash = $1 AND outpoint_index = $2`

	res, err := db.Exec(updateStatus, "abcd", 0, 4, "2026-06-17T01:00:00Z")
	if err != nil {
		t.Fatalf("exec out-of-order update: %v", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("rows affected: %v", err)
	}
	if affected != 1 {
		t.Fatalf(
			"expected 1 row updated, got %d: positional $N mis-bound on the exec path",
			affected,
		)
	}

	var (
		status     int
		lastUpdate string
	)
	if err := db.QueryRow(
		`SELECT status, last_update_time FROM vtxos
		 WHERE outpoint_hash = ? AND outpoint_index = ?`,
		"abcd", 0,
	).Scan(&status, &lastUpdate); err != nil {
		t.Fatalf("read back vtxo row: %v", err)
	}
	if status != 4 || lastUpdate != "2026-06-17T01:00:00Z" {
		t.Fatalf(
			"unexpected row after update: status=%d last_update_time=%q",
			status, lastUpdate,
		)
	}
}

func TestBrowserProtocolMetadataE2E(t *testing.T) {
	bridgeProtocol := js.Global().Get("sqliteBridge").Get("protocolVersion")
	if bridgeProtocol.IsUndefined() {
		t.Fatal("sqliteBridge.protocolVersion is missing")
	}
	if got := bridgeProtocol.Int(); got != ProtocolVersion {
		t.Fatalf("bridge protocol mismatch: got %d want %d", got, ProtocolVersion)
	}
}

func TestBrowserBridgeNotLoadedErrorE2E(t *testing.T) {
	global := js.Global()
	original := global.Get("sqliteBridge")
	global.Set("sqliteBridge", js.Undefined())
	defer global.Set("sqliteBridge", original)

	if _, err := NewBridgeAdapter(); !errors.Is(err, ErrBridgeNotLoaded) {
		t.Fatalf("expected ErrBridgeNotLoaded, got %v", err)
	}
}

func TestBrowserRequirePersistentRejectsMemoryE2E(t *testing.T) {
	db, err := Open(&Options{File: ":memory:", VFS: "memory", RequirePersistent: true})
	if err != nil {
		t.Fatalf("open memory db handle: %v", err)
	}
	defer db.Close()

	if err := db.PingContext(context.Background()); !errors.Is(err, ErrPersistentRequired) {
		t.Fatalf("expected ErrPersistentRequired, got %v", err)
	}
}

func TestBrowserDuplicateOpenE2E(t *testing.T) {
	filename := fmt.Sprintf("/browser-duplicate-open-%d.db", time.Now().UnixNano())
	db1, err := Open(&Options{File: filename, VFS: "opfs", RequirePersistent: true})
	if err != nil {
		t.Fatalf("open first db handle: %v", err)
	}
	defer db1.Close()
	if err := db1.PingContext(context.Background()); err != nil {
		t.Fatalf("ping first db: %v", err)
	}

	db2, err := Open(&Options{File: filename, VFS: "opfs", RequirePersistent: true})
	if err != nil {
		t.Fatalf("open second db handle: %v", err)
	}
	defer db2.Close()

	if err := db2.PingContext(context.Background()); !errors.Is(err, ErrDuplicateOpen) {
		t.Fatalf("expected ErrDuplicateOpen, got %v", err)
	}
}

func TestBrowserUnsupportedVFSE2E(t *testing.T) {
	db, err := Open(&Options{File: "/unsupported-vfs.db", VFS: "definitely-not-a-vfs"})
	if err != nil {
		t.Fatalf("open unsupported vfs db handle: %v", err)
	}
	defer db.Close()

	if err := db.PingContext(context.Background()); !errors.Is(err, ErrUnsupportedVFS) {
		t.Fatalf("expected ErrUnsupportedVFS, got %v", err)
	}
}

func TestBrowserDriverOPFSWebLocksE2E(t *testing.T) {
	filename := fmt.Sprintf("/browser-opfs-wl-%d.db", time.Now().UnixNano())
	db, err := Open(&Options{File: filename, VFS: "opfs-wl", RequirePersistent: true, BusyTimeout: 5000})
	if err != nil {
		t.Fatalf("open opfs-wl db handle: %v", err)
	}
	defer db.Close()

	if err := db.PingContext(context.Background()); err != nil {
		if errors.Is(err, ErrUnsupportedVFS) || strings.Contains(err.Error(), "Atomics.waitAsync") {
			t.Skipf("opfs-wl is not available in this browser: %v", err)
		}
		t.Fatalf("ping opfs-wl db: %v", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("get opfs-wl conn: %v", err)
	}

	vfs, err := GetVFSType(conn)
	conn.Close()
	if err != nil {
		t.Fatalf("get opfs-wl vfs type: %v", err)
	}
	if vfs != VFSTypeOPFSWebLocks {
		t.Fatalf("expected opfs-wl VFS, got %s", vfs)
	}

	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS wl_values (id INTEGER PRIMARY KEY, label TEXT);
INSERT INTO wl_values(label) VALUES ('ok');
UPDATE wl_values SET label = 'done' WHERE id = 1;
`); err != nil {
		t.Fatalf("exec opfs-wl SQL: %v", err)
	}

	var label string
	if err := db.QueryRow(`SELECT label FROM wl_values WHERE id = 1`).Scan(&label); err != nil {
		t.Fatalf("query opfs-wl row: %v", err)
	}
	if label != "done" {
		t.Fatalf("unexpected opfs-wl label: %q", label)
	}
}

func TestBrowserDriverSAHPoolE2E(t *testing.T) {
	filename := fmt.Sprintf("/browser-sahpool-%d.db", time.Now().UnixNano())
	db, err := Open(&Options{File: filename, VFS: "opfs-sahpool", BusyTimeout: 5000})
	if err != nil {
		t.Fatalf("open opfs-sahpool db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
-- exercise explicit opfs-sahpool VFS and multi-statement execution
CREATE TABLE IF NOT EXISTS sah_values (id INTEGER PRIMARY KEY, label TEXT);
INSERT INTO sah_values(label) VALUES ('ok');
UPDATE sah_values SET label = 'done' WHERE id = 1;
`); err != nil {
		t.Fatalf("exec multi-statement sahpool SQL: %v", err)
	}

	var label string
	if err := db.QueryRow(`SELECT label FROM sah_values WHERE id = 1`).Scan(&label); err != nil {
		t.Fatalf("query sahpool row: %v", err)
	}
	if label != "done" {
		t.Fatalf("unexpected sahpool label: %q", label)
	}
}

func TestBrowserMigrateDriverE2E(t *testing.T) {
	filename := fmt.Sprintf("/browser-migrate-%d.db", time.Now().UnixNano())
	db, err := Open(&Options{File: filename, VFS: "opfs", BusyTimeout: 5000})
	if err != nil {
		t.Fatalf("open migrate db: %v", err)
	}
	defer db.Close()

	driver, err := NewMigrateDriver(db)
	if err != nil {
		t.Fatalf("new migrate driver: %v", err)
	}

	migration := strings.NewReader(`
-- Create users table
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL
);

-- Initialize an existing row
INSERT INTO users(username, email) VALUES ('kon', 'kon@example.com');
UPDATE users SET email = 'updated@example.com' WHERE username = 'kon';
`)
	if err := driver.Run(migration); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	var email string
	if err := db.QueryRow(`SELECT email FROM users WHERE username = ?`, "kon").Scan(&email); err != nil {
		t.Fatalf("query migrated row: %v", err)
	}
	if email != "updated@example.com" {
		t.Fatalf("unexpected migrated email: %q", email)
	}
}

func TestBrowserMigrateDriverContractE2E(t *testing.T) {
	filename := fmt.Sprintf("/browser-migrate-contract-%d.db", time.Now().UnixNano())
	db, err := Open(&Options{File: filename, VFS: "opfs", BusyTimeout: 5000})
	if err != nil {
		t.Fatalf("open migrate contract db: %v", err)
	}
	defer db.Close()

	driver, err := NewMigrateDriver(db)
	if err != nil {
		t.Fatalf("new migrate driver: %v", err)
	}

	migratetesting.Test(t, driver, []byte(`
CREATE TABLE migrate_contract (
    id INTEGER PRIMARY KEY,
    label TEXT NOT NULL
);
INSERT INTO migrate_contract(id, label) VALUES (1, 'ok');
`))
}

func TestBrowserExampleMigrationsE2E(t *testing.T) {
	filename := fmt.Sprintf("/browser-example-migrations-%d.db", time.Now().UnixNano())
	db, err := Open(&Options{File: filename, VFS: "opfs", BusyTimeout: 5000})
	if err != nil {
		t.Fatalf("open example migrations db: %v", err)
	}
	defer db.Close()

	driver, err := NewMigrateDriver(db)
	if err != nil {
		t.Fatalf("new migrate driver: %v", err)
	}

	for _, name := range []string{
		"example/migrations/001_initial_schema.up.sql",
		"example/migrations/002_add_updated_at.up.sql",
		"example/migrations/003_add_blob_fields.up.sql",
	} {
		data, err := browserMigrationFS.ReadFile(name)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if err := driver.Run(bytes.NewReader(data)); err != nil {
			t.Fatalf("run migration %s: %v", name, err)
		}
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		t.Fatalf("query users after example migrations: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM attachments`).Scan(&count); err != nil {
		t.Fatalf("query attachments after example migrations: %v", err)
	}
}

func TestBrowserGolangMigrateExampleE2E(t *testing.T) {
	filename := fmt.Sprintf("/browser-golang-migrate-%d.db", time.Now().UnixNano())
	db, err := Open(&Options{File: filename, VFS: "opfs", BusyTimeout: 5000})
	if err != nil {
		t.Fatalf("open golang-migrate db: %v", err)
	}
	defer db.Close()

	sourceDriver, err := iofs.New(browserMigrationFS, "example/migrations")
	if err != nil {
		t.Fatalf("new source driver: %v", err)
	}
	dbDriver, err := NewMigrateDriver(db)
	if err != nil {
		t.Fatalf("new migrate driver: %v", err)
	}
	m, err := migrate.NewWithInstance("iofs", sourceDriver, "wasmsqlite", dbDriver)
	if err != nil {
		t.Fatalf("new migrate instance: %v", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate up: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		t.Fatalf("query users after golang migrate: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM attachments`).Scan(&count); err != nil {
		t.Fatalf("query attachments after golang migrate: %v", err)
	}
}

func TestBrowserEmptyBlobRoundTripE2E(t *testing.T) {
	db, err := Open(&Options{File: ":memory:", VFS: "memory"})
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE blob_values (id INTEGER PRIMARY KEY, data BLOB)`); err != nil {
		t.Fatalf("create blob schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO blob_values(id, data) VALUES (?, ?)`, 1, []byte{}); err != nil {
		t.Fatalf("insert empty blob: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO blob_values(id, data) VALUES (?, ?)`, 2, []byte(nil)); err != nil {
		t.Fatalf("insert null blob: %v", err)
	}

	var length int64
	var isNull bool
	var data []byte
	if err := db.QueryRow(`SELECT length(data), data IS NULL, data FROM blob_values WHERE id = 1`).Scan(&length, &isNull, &data); err != nil {
		t.Fatalf("query empty blob: %v", err)
	}
	if length != 0 || isNull || data == nil || len(data) != 0 {
		t.Fatalf("empty blob mismatch: length=%d isNull=%v data=%v", length, isNull, data)
	}

	if err := db.QueryRow(`SELECT data IS NULL FROM blob_values WHERE id = 2`).Scan(&isNull); err != nil {
		t.Fatalf("query null blob: %v", err)
	}
	if !isNull {
		t.Fatal("nil []byte should bind as NULL")
	}
}

func TestBrowserInt64RoundTripE2E(t *testing.T) {
	db, err := Open(&Options{File: ":memory:", VFS: "memory"})
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE integer_values (id INTEGER PRIMARY KEY, value INTEGER NOT NULL)`); err != nil {
		t.Fatalf("create integer schema: %v", err)
	}

	values := []int64{
		42,
		1<<53 - 1,
		1 << 53,
		math.MaxInt64,
	}
	for i, value := range values {
		if _, err := db.Exec(`INSERT INTO integer_values(id, value) VALUES (?, ?)`, i+1, value); err != nil {
			t.Fatalf("insert int64 %d: %v", value, err)
		}
	}

	for i, want := range values {
		var got int64
		var sqliteType string
		if err := db.QueryRow(`SELECT value, typeof(value) FROM integer_values WHERE id = ?`, i+1).Scan(&got, &sqliteType); err != nil {
			t.Fatalf("query int64 %d: %v", want, err)
		}
		if got != want || sqliteType != "integer" {
			t.Fatalf("int64 mismatch: want=%d got=%d sqliteType=%q", want, got, sqliteType)
		}
	}
}

func TestBrowserParseTimeE2E(t *testing.T) {
	db, err := Open(&Options{File: ":memory:", VFS: "memory", ParseTime: true})
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE time_values (id INTEGER PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatalf("create time schema: %v", err)
	}

	withNanos := time.Date(2026, 5, 14, 13, 45, 9, 123456789, time.UTC)
	values := []struct {
		id   int
		arg  interface{}
		want time.Time
	}{
		{1, withNanos, withNanos},
		{2, "2026-05-14T13:45:09Z", time.Date(2026, 5, 14, 13, 45, 9, 0, time.UTC)},
		{3, "2026-05-14 13:45:09.123456789", withNanos},
		{4, "2026-05-14 13:45:09", time.Date(2026, 5, 14, 13, 45, 9, 0, time.UTC)},
		{5, "2026-05-14", time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)},
	}
	for _, value := range values {
		if _, err := db.Exec(`INSERT INTO time_values(id, value) VALUES (?, ?)`, value.id, value.arg); err != nil {
			t.Fatalf("insert time %d: %v", value.id, err)
		}
	}

	for _, value := range values {
		var got time.Time
		if err := db.QueryRow(`SELECT value FROM time_values WHERE id = ?`, value.id).Scan(&got); err != nil {
			t.Fatalf("scan time %d: %v", value.id, err)
		}
		if !got.Equal(value.want) {
			t.Fatalf("time mismatch for id=%d: want=%s got=%s", value.id, value.want.Format(time.RFC3339Nano), got.Format(time.RFC3339Nano))
		}
	}

	var nullable sql.NullTime
	if err := db.QueryRow(`SELECT value FROM time_values WHERE id = 1`).Scan(&nullable); err != nil {
		t.Fatalf("scan null time: %v", err)
	}
	if !nullable.Valid || !nullable.Time.Equal(withNanos) {
		t.Fatalf("unexpected null time: valid=%v value=%s", nullable.Valid, nullable.Time.Format(time.RFC3339Nano))
	}
}

func TestBrowserSQLShapesE2E(t *testing.T) {
	db, err := Open(&Options{File: ":memory:", VFS: "memory", ParseTime: true})
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
CREATE TABLE shape_users (
  id INTEGER PRIMARY KEY,
  username TEXT NOT NULL,
  avatar BLOB,
  created_at TEXT,
  nullable TEXT
);
CREATE TABLE shape_posts (
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL,
  title TEXT NOT NULL,
  published BOOLEAN,
  FOREIGN KEY(user_id) REFERENCES shape_users(id)
);
`); err != nil {
		t.Fatalf("create SQL shape schema: %v", err)
	}

	createdAt := time.Date(2026, 5, 14, 16, 1, 2, 345678900, time.UTC)
	var userID int64
	if err := db.QueryRow(
		`INSERT INTO shape_users(username, avatar, created_at, nullable) VALUES (?, ?, ?, ?) RETURNING id`,
		"alice", []byte{}, createdAt, nil,
	).Scan(&userID); err != nil {
		t.Fatalf("INSERT RETURNING explicit id: %v", err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin SQL shape transaction: %v", err)
	}
	var postID int64
	if err := tx.QueryRow(
		`INSERT INTO shape_posts(user_id, title, published) VALUES (?, ?, ?) RETURNING id`,
		userID, "draft", false,
	).Scan(&postID); err != nil {
		tx.Rollback()
		t.Fatalf("INSERT RETURNING post in transaction: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit SQL shape transaction: %v", err)
	}

	var (
		aliasName string
		title     string
		when      time.Time
		nullable  sql.NullString
		avatar    []byte
	)
	if err := db.QueryRow(`
WITH recent_posts AS (
  SELECT id, user_id, title FROM shape_posts WHERE id = ?
)
SELECT u.username AS alias_name, rp.title AS post_title, u.created_at, u.nullable, u.avatar
FROM recent_posts rp
JOIN shape_users u ON u.id = rp.user_id
WHERE u.id = ?
`, postID, userID).Scan(&aliasName, &title, &when, &nullable, &avatar); err != nil {
		t.Fatalf("CTE JOIN SELECT aliases explicit columns: %v", err)
	}
	if aliasName != "alice" || title != "draft" || !when.Equal(createdAt) || nullable.Valid || avatar == nil || len(avatar) != 0 {
		t.Fatalf("unexpected SQL shape values: alias=%q title=%q when=%s nullable=%v avatar=%v", aliasName, title, when.Format(time.RFC3339Nano), nullable, avatar)
	}

	var updatedTitle string
	if err := db.QueryRow(
		`UPDATE shape_posts SET title = ? WHERE id = ? RETURNING title AS updated_title`,
		"published", postID,
	).Scan(&updatedTitle); err != nil {
		t.Fatalf("UPDATE RETURNING alias: %v", err)
	}
	if updatedTitle != "published" {
		t.Fatalf("unexpected updated title: %q", updatedTitle)
	}

	var deletedID int64
	if err := db.QueryRow(`DELETE FROM shape_posts WHERE id = ? RETURNING id`, postID).Scan(&deletedID); err != nil {
		t.Fatalf("DELETE RETURNING id: %v", err)
	}
	if deletedID != postID {
		t.Fatalf("unexpected deleted id: got %d want %d", deletedID, postID)
	}
}

func TestBrowserGeneratedQueriesE2E(t *testing.T) {
	ctx := context.Background()
	db, err := Open(&Options{File: ":memory:", VFS: "memory", ParseTime: true})
	if err != nil {
		t.Fatalf("open generated-query db: %v", err)
	}
	defer db.Close()

	driver, err := NewMigrateDriver(db)
	if err != nil {
		t.Fatalf("new migrate driver for generated queries: %v", err)
	}
	for _, name := range []string{
		"example/migrations/001_initial_schema.up.sql",
		"example/migrations/002_add_updated_at.up.sql",
		"example/migrations/003_add_blob_fields.up.sql",
	} {
		data, err := browserMigrationFS.ReadFile(name)
		if err != nil {
			t.Fatalf("read generated-query migration %s: %v", name, err)
		}
		if err := driver.Run(bytes.NewReader(data)); err != nil {
			t.Fatalf("run generated-query migration %s: %v", name, err)
		}
	}

	q := database.New(db)
	user, err := q.CreateUser(ctx, database.CreateUserParams{
		Username: "generated-alice",
		Email:    "generated-alice@example.com",
	})
	if err != nil {
		t.Fatalf("generated CreateUser: %v", err)
	}
	if err := q.UpdateUserAvatar(ctx, database.UpdateUserAvatarParams{
		ID:       user.ID,
		Avatar:   []byte{},
		Metadata: []byte{0x01, 0x02, 0x03},
	}); err != nil {
		t.Fatalf("generated UpdateUserAvatar with empty BLOB: %v", err)
	}
	userWithAvatar, err := q.GetUserWithAvatar(ctx, user.ID)
	if err != nil {
		t.Fatalf("generated GetUserWithAvatar: %v", err)
	}
	if userWithAvatar.Avatar == nil || len(userWithAvatar.Avatar) != 0 || !bytes.Equal(userWithAvatar.Metadata, []byte{0x01, 0x02, 0x03}) {
		t.Fatalf("generated BLOB mismatch: avatar=%v metadata=%x", userWithAvatar.Avatar, userWithAvatar.Metadata)
	}

	post, err := q.CreatePost(ctx, database.CreatePostParams{
		UserID:    user.ID,
		Title:     "generated draft",
		Content:   sql.NullString{String: "body", Valid: true},
		Published: sql.NullBool{Bool: false, Valid: true},
	})
	if err != nil {
		t.Fatalf("generated CreatePost RETURNING: %v", err)
	}
	joinedPost, err := q.GetPost(ctx, post.ID)
	if err != nil {
		t.Fatalf("generated GetPost JOIN: %v", err)
	}
	if joinedPost.Username != user.Username || joinedPost.Title != post.Title {
		t.Fatalf("generated GetPost mismatch: username=%q title=%q", joinedPost.Username, joinedPost.Title)
	}
	if !joinedPost.CreatedAt.Valid {
		t.Fatalf("generated created_at time was not parsed: created=%v", joinedPost.CreatedAt)
	}
	posts, err := q.ListPostsByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("generated ListPostsByUser: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("generated ListPostsByUser count=%d", len(posts))
	}
	if err := q.UpdatePost(ctx, database.UpdatePostParams{
		ID:        post.ID,
		Title:     "generated published",
		Content:   sql.NullString{},
		Published: sql.NullBool{Bool: true, Valid: true},
	}); err != nil {
		t.Fatalf("generated UpdatePost: %v", err)
	}
	updatedPost, err := q.GetPost(ctx, post.ID)
	if err != nil {
		t.Fatalf("generated GetPost after UpdatePost: %v", err)
	}
	if updatedPost.Title != "generated published" || updatedPost.Content.Valid {
		t.Fatalf("generated updated post mismatch: title=%q content=%v updated=%v", updatedPost.Title, updatedPost.Content, updatedPost.UpdatedAt)
	}

	attachment, err := q.CreateAttachment(ctx, database.CreateAttachmentParams{
		UserID:      user.ID,
		Filename:    "payload.bin",
		ContentType: sql.NullString{String: "application/octet-stream", Valid: true},
		Data:        []byte{0x00, 0x7f, 0xff},
		Thumbnail:   []byte{},
		Size:        sql.NullInt64{Int64: 3, Valid: true},
		Checksum:    []byte{0xaa, 0xbb},
	})
	if err != nil {
		t.Fatalf("generated CreateAttachment RETURNING BLOB: %v", err)
	}
	data, err := q.GetAttachmentData(ctx, attachment.ID)
	if err != nil {
		t.Fatalf("generated GetAttachmentData: %v", err)
	}
	if !bytes.Equal(data, []byte{0x00, 0x7f, 0xff}) {
		t.Fatalf("generated attachment data mismatch: %x", data)
	}
	attachments, err := q.ListAttachmentsByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("generated ListAttachmentsByUser: %v", err)
	}
	if len(attachments) != 1 {
		t.Fatalf("generated ListAttachmentsByUser count=%d", len(attachments))
	}
	if err := q.UpdateAttachmentThumbnail(ctx, database.UpdateAttachmentThumbnailParams{
		ID:        attachment.ID,
		Thumbnail: []byte{0x10, 0x20},
	}); err != nil {
		t.Fatalf("generated UpdateAttachmentThumbnail: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("generated transaction begin: %v", err)
	}
	txq := q.WithTx(tx)
	txPost, err := txq.CreatePost(ctx, database.CreatePostParams{
		UserID:    user.ID,
		Title:     "generated tx",
		Content:   sql.NullString{},
		Published: sql.NullBool{Bool: false, Valid: true},
	})
	if err != nil {
		tx.Rollback()
		t.Fatalf("generated transactional CreatePost: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("generated transaction commit: %v", err)
	}
	if _, err := q.GetPost(ctx, txPost.ID); err != nil {
		t.Fatalf("generated transactional GetPost after commit: %v", err)
	}

	if err := q.DeleteAttachment(ctx, attachment.ID); err != nil {
		t.Fatalf("generated DeleteAttachment: %v", err)
	}
	if err := q.DeletePost(ctx, post.ID); err != nil {
		t.Fatalf("generated DeletePost: %v", err)
	}
}

func exerciseDatabaseSQL(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.Exec(`
CREATE TABLE typed_values (
  id INTEGER PRIMARY KEY,
  label TEXT NOT NULL,
  nil_value TEXT,
  bool_value BOOLEAN,
  int_value INTEGER,
  float_value REAL,
  text_value TEXT,
  time_value TEXT,
  blob_value BLOB
)`)
	if err != nil {
		t.Fatalf("create typed_values: %v", err)
	}

	now := time.Date(2026, 5, 14, 12, 30, 45, 0, time.UTC)
	blob := []byte{0x00, 0x01, 0x7f, 0x80, 0xff}
	res, err := db.Exec(
		`INSERT INTO typed_values(label, nil_value, bool_value, int_value, float_value, text_value, time_value, blob_value)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"first", nil, true, int64(42), 3.5, "hello", now, blob,
	)
	if err != nil {
		t.Fatalf("insert typed row: %v", err)
	}
	if rows, err := res.RowsAffected(); err != nil || rows != 1 {
		t.Fatalf("rows affected = %d, %v", rows, err)
	}

	var (
		nilValue  sql.NullString
		boolInt   int64
		intValue  int64
		floatVal  float64
		textValue string
		timeValue string
		blobValue []byte
	)
	err = db.QueryRow(`
SELECT nil_value, bool_value, int_value, float_value, text_value, time_value, blob_value
FROM typed_values WHERE label = ?`, "first").Scan(
		&nilValue, &boolInt, &intValue, &floatVal, &textValue, &timeValue, &blobValue,
	)
	if err != nil {
		t.Fatalf("query typed row: %v", err)
	}
	if nilValue.Valid {
		t.Fatalf("expected nil_value to scan as NULL")
	}
	if boolInt != 1 || intValue != 42 || floatVal != 3.5 || textValue != "hello" {
		t.Fatalf("unexpected typed values: bool=%d int=%d float=%f text=%q", boolInt, intValue, floatVal, textValue)
	}
	if timeValue != now.Format(time.RFC3339) {
		t.Fatalf("unexpected time value: %q", timeValue)
	}
	if !bytes.Equal(blobValue, blob) {
		t.Fatalf("blob mismatch: got %x want %x", blobValue, blob)
	}

	if _, err := db.Exec(`UPDATE typed_values SET text_value = ? WHERE label = ?`, "updated", "first"); err != nil {
		t.Fatalf("update row: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM typed_values WHERE label = ?`, "missing"); err != nil {
		t.Fatalf("delete missing row: %v", err)
	}
}

func exerciseTransactions(t *testing.T, db *sql.DB) {
	t.Helper()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin rollback tx: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO typed_values(label, int_value) VALUES (?, ?)`, "rollback", 1); err != nil {
		t.Fatalf("insert rollback row: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM typed_values WHERE label = ?`, "rollback").Scan(&count); err != nil {
		t.Fatalf("query rollback count: %v", err)
	}
	if count != 0 {
		t.Fatalf("rollback row persisted")
	}

	tx, err = db.Begin()
	if err != nil {
		t.Fatalf("begin commit tx: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO typed_values(label, int_value) VALUES (?, ?)`, "commit", 2); err != nil {
		t.Fatalf("insert commit row: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM typed_values WHERE label = ?`, "commit").Scan(&count); err != nil {
		t.Fatalf("query commit count: %v", err)
	}
	if count != 1 {
		t.Fatalf("commit row missing")
	}
}

func exerciseDumpLoad(t *testing.T, db *sql.DB) {
	t.Helper()

	dump, err := DumpDatabase(db)
	if err != nil {
		t.Fatalf("dump database: %v", err)
	}
	if !bytes.Contains([]byte(dump), []byte("X'00017f80ff'")) {
		t.Fatalf("dump does not contain BLOB hex literal:\n%s", dump)
	}

	loaded, err := Open(&Options{File: ":memory:", VFS: "memory"})
	if err != nil {
		t.Fatalf("open load target: %v", err)
	}
	defer loaded.Close()

	if err := LoadDatabase(loaded, dump); err != nil {
		t.Fatalf("load database: %v", err)
	}

	var blob []byte
	if err := loaded.QueryRow(`SELECT blob_value FROM typed_values WHERE label = ?`, "first").Scan(&blob); err != nil {
		t.Fatalf("query loaded blob: %v", err)
	}
	if !bytes.Equal(blob, []byte{0x00, 0x01, 0x7f, 0x80, 0xff}) {
		t.Fatalf("loaded blob mismatch: %x", blob)
	}
}
