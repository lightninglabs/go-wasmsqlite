//go:build js && wasm

package wasmsqlite

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratetesting "github.com/golang-migrate/migrate/v4/database/testing"
	"github.com/golang-migrate/migrate/v4/source/iofs"
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

func TestBrowserRequirePersistentRejectsMemoryE2E(t *testing.T) {
	db, err := Open(&Options{File: ":memory:", VFS: "memory", RequirePersistent: true})
	if err != nil {
		t.Fatalf("open memory db handle: %v", err)
	}
	defer db.Close()

	if err := db.PingContext(context.Background()); err == nil {
		t.Fatal("expected require_persistent memory open to fail")
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
