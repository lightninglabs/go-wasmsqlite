//go:build js && wasm

package wasmsqlite

import (
	"context"
	"database/sql/driver"
	"errors"
	"time"
)

var ErrTxDone = errors.New("sql: transaction has already been committed or rolled back")

// Tx implements the database/sql/driver.Tx interface
type Tx struct {
	conn *Conn
}

// Commit implements driver.Tx
func (tx *Tx) Commit() error {
	if tx.conn.queue == nil {
		return driver.ErrBadConn
	}
	
	if !tx.conn.inTx {
		return ErrTxDone
	}
	
	request := createJSRequest(0, "commit", nil)
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	response, err := tx.conn.queue.SendRequest(ctx, request)
	if err != nil {
		return err
	}
	
	if response.Error != nil {
		return response.Error
	}
	
	tx.conn.inTx = false
	return nil
}

// Rollback implements driver.Tx
func (tx *Tx) Rollback() error {
	if tx.conn.queue == nil {
		return driver.ErrBadConn
	}
	
	if !tx.conn.inTx {
		return ErrTxDone
	}
	
	request := createJSRequest(0, "rollback", nil)
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	response, err := tx.conn.queue.SendRequest(ctx, request)
	if err != nil {
		return err
	}
	
	if response.Error != nil {
		return response.Error
	}
	
	tx.conn.inTx = false
	return nil
}