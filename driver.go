//go:build js && wasm

package wasmsqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"syscall/js"
)

func init() {
	sql.Register("wasmsqlite", &Driver{})
}

// Driver implements the database/sql/driver.Driver interface
type Driver struct{}

// Open opens a new database connection
func (d *Driver) Open(name string) (driver.Conn, error) {
	return d.OpenConnector(name).Connect(context.Background())
}

// OpenConnector implements driver.DriverContext
func (d *Driver) OpenConnector(name string) driver.Connector {
	return &Connector{dsn: name}
}

// Connector implements the driver.Connector interface
type Connector struct {
	dsn string
}

// Connect establishes a connection to the database
func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	opts, err := parseDSN(c.dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid DSN: %w", err)
	}

	bridge, _, err := createWorker(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create bridge: %w", err)
	}

	// Create bridge adapter
	adapter, err := NewBridgeAdapter()
	if err != nil {
		return nil, fmt.Errorf("failed to create bridge adapter: %w", err)
	}

	// Open database through bridge
	vfsType, err := adapter.Open(opts.File, opts.VFS)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	conn := &Conn{
		bridge:  bridge,
		adapter: adapter,
		vfsType: vfsType,
	}

	return conn, nil
}

// Driver returns the underlying driver
func (c *Connector) Driver() driver.Driver {
	return &Driver{}
}

// Conn implements the database/sql/driver.Conn interface
type Conn struct {
	bridge  js.Value
	adapter *BridgeAdapter
	inTx    bool
	vfsType string
}

// Prepare implements driver.Conn
func (c *Conn) Prepare(query string) (driver.Stmt, error) {
	if c.adapter == nil {
		return nil, driver.ErrBadConn
	}

	return &Stmt{
		conn:  c,
		query: query,
	}, nil
}

// Close implements driver.Conn
func (c *Conn) Close() error {
	if c.adapter != nil {
		// Close the database through bridge
		err := c.adapter.Close()
		c.adapter = nil
		return err
	}
	return nil
}

// Begin implements driver.Conn
func (c *Conn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

// BeginTx implements driver.ConnBeginTx
func (c *Conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if c.adapter == nil {
		return nil, driver.ErrBadConn
	}

	if c.inTx {
		return nil, fmt.Errorf("already in transaction")
	}

	// SQLite doesn't support read-only transactions or isolation levels in the same way
	// We'll just start a regular transaction
	err := c.adapter.Begin()
	if err != nil {
		return nil, err
	}

	c.inTx = true

	return &Tx{conn: c}, nil
}

// ExecContext implements driver.ExecerContext
func (c *Conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.adapter == nil {
		return nil, driver.ErrBadConn
	}

	// Convert params to interface{} slice
	paramIfaces := make([]interface{}, len(args))
	for i, arg := range args {
		paramIfaces[i] = arg.Value
	}

	rowsAffected, lastInsertID, err := c.adapter.Exec(query, paramIfaces)
	if err != nil {
		return nil, err
	}

	rowsAff := int64(rowsAffected)
	lastInsID := int64(lastInsertID)
	return &Result{
		rowsAffected: &rowsAff,
		lastInsertID: &lastInsID,
	}, nil
}

// QueryContext implements driver.QueryerContext
func (c *Conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c.adapter == nil {
		return nil, driver.ErrBadConn
	}

	// Convert params to interface{} slice
	paramIfaces := make([]interface{}, len(args))
	for i, arg := range args {
		paramIfaces[i] = arg.Value
	}

	columns, rows, err := c.adapter.Query(query, paramIfaces)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Query returned %d columns: %v\n", len(columns), columns)
	fmt.Printf("Query returned %d rows\n", len(rows))
	if len(rows) > 0 {
		fmt.Printf("First row: %v\n", rows[0])
	}

	return &Rows{
		columns: columns,
		rows:    rows,
		pos:     0,
	}, nil
}

// Ping implements driver.Pinger
func (c *Conn) Ping(ctx context.Context) error {
	if c.adapter == nil {
		return driver.ErrBadConn
	}

	// Try a simple query to check if connection is alive
	_, err := c.QueryContext(ctx, "SELECT 1", nil)
	return err
}

// Stmt implements the database/sql/driver.Stmt interface
type Stmt struct {
	conn  *Conn
	query string
}

// Close implements driver.Stmt
func (s *Stmt) Close() error {
	return nil
}

// NumInput implements driver.Stmt
func (s *Stmt) NumInput() int {
	return -1 // Unknown number of parameters
}

// Exec implements driver.Stmt
func (s *Stmt) Exec(args []driver.Value) (driver.Result, error) {
	namedArgs := make([]driver.NamedValue, len(args))
	for i, arg := range args {
		namedArgs[i] = driver.NamedValue{
			Ordinal: i + 1,
			Value:   arg,
		}
	}
	return s.conn.ExecContext(context.Background(), s.query, namedArgs)
}

// Query implements driver.Stmt
func (s *Stmt) Query(args []driver.Value) (driver.Rows, error) {
	namedArgs := make([]driver.NamedValue, len(args))
	for i, arg := range args {
		namedArgs[i] = driver.NamedValue{
			Ordinal: i + 1,
			Value:   arg,
		}
	}
	return s.conn.QueryContext(context.Background(), s.query, namedArgs)
}

// ExecContext implements driver.StmtExecContext
func (s *Stmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.conn.ExecContext(ctx, s.query, args)
}

// QueryContext implements driver.StmtQueryContext
func (s *Stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.conn.QueryContext(ctx, s.query, args)
}

// Result implements the database/sql/driver.Result interface
type Result struct {
	rowsAffected *int64
	lastInsertID *int64
}

// LastInsertId implements driver.Result
func (r *Result) LastInsertId() (int64, error) {
	if r.lastInsertID == nil {
		return 0, fmt.Errorf("no last insert ID available")
	}
	return *r.lastInsertID, nil
}

// RowsAffected implements driver.Result
func (r *Result) RowsAffected() (int64, error) {
	if r.rowsAffected == nil {
		return 0, fmt.Errorf("no rows affected count available")
	}
	return *r.rowsAffected, nil
}

// GetVFSType returns the VFS type being used by the connection
func (c *Conn) GetVFSType() VFSType {
	switch c.vfsType {
	case "opfs":
		return VFSTypeOPFS
	case "memory":
		return VFSTypeMemory
	default:
		return VFSTypeUnknown
	}
}

// Dump exports the database as SQL statements
func (c *Conn) Dump(ctx context.Context) (string, error) {
	if c.adapter == nil {
		return "", driver.ErrBadConn
	}

	return c.adapter.Dump()
}

// Load imports SQL statements to restore the database
func (c *Conn) Load(ctx context.Context, dump string) error {
	if c.adapter == nil {
		return driver.ErrBadConn
	}

	return c.adapter.Load(dump)
}
