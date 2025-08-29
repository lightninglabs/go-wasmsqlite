//go:build js && wasm

package wasmsqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"syscall/js"
	"time"

	"github.com/sputn1ck/sqlc-wasm/internal"
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
	
	worker, queue, err := createWorker(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create worker: %w", err)
	}
	
	vfsType, err := openDatabase(queue, opts)
	if err != nil {
		queue.Close()
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	
	conn := &Conn{
		worker:  worker,
		queue:   queue,
		opts:    opts,
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
	worker  js.Value
	queue   *internal.Queue
	opts    *Options
	inTx    bool
	vfsType string
}

// Prepare implements driver.Conn
func (c *Conn) Prepare(query string) (driver.Stmt, error) {
	if c.queue == nil {
		return nil, driver.ErrBadConn
	}
	
	return &Stmt{
		conn:  c,
		query: query,
	}, nil
}

// Close implements driver.Conn
func (c *Conn) Close() error {
	if c.queue != nil {
		// Send close message to Worker
		request := createJSRequest(0, "close", nil)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		
		c.queue.SendRequest(ctx, request)
		
		// Close the queue (this will terminate the Worker)
		err := c.queue.Close()
		c.queue = nil
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
	if c.queue == nil {
		return nil, driver.ErrBadConn
	}
	
	if c.inTx {
		return nil, fmt.Errorf("already in transaction")
	}
	
	// SQLite doesn't support read-only transactions or isolation levels in the same way
	// We'll just start a regular transaction
	request := createJSRequest(0, "begin", nil)
	
	response, err := c.queue.SendRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	
	if response.Error != nil {
		return nil, response.Error
	}
	
	c.inTx = true
	
	return &Tx{conn: c}, nil
}

// ExecContext implements driver.ExecerContext
func (c *Conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.queue == nil {
		return nil, driver.ErrBadConn
	}
	
	params := make([]driver.Value, len(args))
	for i, arg := range args {
		params[i] = arg.Value
	}
	
	request := createJSRequest(0, "exec", map[string]interface{}{
		"sql":    query,
		"params": params,
	})
	
	response, err := c.queue.SendRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	
	if response.Error != nil {
		return nil, response.Error
	}
	
	result, err := parseJSResponse(response.Data)
	if err != nil {
		return nil, err
	}
	
	return &Result{
		rowsAffected: result.RowsAffected,
		lastInsertID: result.LastInsertID,
	}, nil
}

// QueryContext implements driver.QueryerContext  
func (c *Conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c.queue == nil {
		return nil, driver.ErrBadConn
	}
	
	params := make([]driver.Value, len(args))
	for i, arg := range args {
		params[i] = arg.Value
	}
	
	request := createJSRequest(0, "query", map[string]interface{}{
		"sql":    query,
		"params": params,
	})
	
	response, err := c.queue.SendRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	
	if response.Error != nil {
		return nil, response.Error
	}
	
	result, err := parseJSResponse(response.Data)
	if err != nil {
		return nil, err
	}
	
	fmt.Printf("Query returned %d columns: %v\n", len(result.Columns), result.Columns)
	fmt.Printf("Query returned %d rows\n", len(result.Rows))
	if len(result.Rows) > 0 {
		fmt.Printf("First row: %v\n", result.Rows[0])
	}
	
	return &Rows{
		columns: result.Columns,
		rows:    result.Rows,
		pos:     0,
	}, nil
}

// Ping implements driver.Pinger
func (c *Conn) Ping(ctx context.Context) error {
	if c.queue == nil || !c.queue.IsHealthy() {
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
	if c.queue == nil {
		return "", driver.ErrBadConn
	}
	
	request := createJSRequest(0, "dump", nil)
	
	response, err := c.queue.SendRequest(ctx, request)
	if err != nil {
		return "", err
	}
	
	if response.Error != nil {
		return "", response.Error
	}
	
	// Extract dump from response
	if !response.Data.IsNull() && !response.Data.IsUndefined() {
		dump := response.Data.Get("dump")
		if dump.Truthy() {
			return dump.String(), nil
		}
	}
	
	return "", fmt.Errorf("no dump data received")
}

// Load imports SQL statements to restore the database
func (c *Conn) Load(ctx context.Context, dump string) error {
	if c.queue == nil {
		return driver.ErrBadConn
	}
	
	request := createJSRequest(0, "load", map[string]interface{}{
		"sql": dump,
	})
	
	response, err := c.queue.SendRequest(ctx, request)
	if err != nil {
		return err
	}
	
	return response.Error
}