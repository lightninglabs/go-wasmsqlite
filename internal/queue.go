//go:build js && wasm

package internal

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"syscall/js"
)

// Request represents a queued request to the Worker
type Request struct {
	ID       int64
	Response chan *Response
	Context  context.Context
	Cancel   context.CancelFunc
}

// Response represents a response from the Worker
type Response struct {
	Data  js.Value
	Error error
}

// Queue manages serialized requests to the Worker
type Queue struct {
	mu       sync.Mutex
	worker   js.Value
	requests map[int64]*Request
	nextID   int64
	closed   bool
}

// NewQueue creates a new request queue for the given Worker
func NewQueue(worker js.Value) *Queue {
	q := &Queue{
		worker:   worker,
		requests: make(map[int64]*Request),
		nextID:   1,
	}
	
	// Set up message handler
	messageHandler := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) > 0 {
			event := args[0]
			data := event.Get("data")
			q.handleResponse(data)
		}
		return nil
	})
	
	worker.Set("onmessage", messageHandler)
	
	return q
}

// SendRequest sends a request to the Worker and waits for response
func (q *Queue) SendRequest(ctx context.Context, request js.Value) (*Response, error) {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return nil, fmt.Errorf("queue is closed")
	}
	
	id := atomic.AddInt64(&q.nextID, 1)
	request.Set("id", id)
	
	// Create context with cancellation
	reqCtx, cancel := context.WithCancel(ctx)
	
	req := &Request{
		ID:       id,
		Response: make(chan *Response, 1),
		Context:  reqCtx,
		Cancel:   cancel,
	}
	
	q.requests[id] = req
	q.mu.Unlock()
	
	// Send message to Worker
	q.worker.Call("postMessage", request)
	
	// Wait for response or cancellation
	select {
	case response := <-req.Response:
		q.mu.Lock()
		delete(q.requests, id)
		q.mu.Unlock()
		cancel()
		return response, nil
		
	case <-reqCtx.Done():
		q.mu.Lock()
		delete(q.requests, id)
		q.mu.Unlock()
		cancel()
		return nil, reqCtx.Err()
	}
}

// handleResponse handles incoming responses from the Worker
func (q *Queue) handleResponse(data js.Value) {
	id := int64(data.Get("id").Float())
	
	q.mu.Lock()
	req, exists := q.requests[id]
	q.mu.Unlock()
	
	if !exists {
		// Request may have been cancelled or timed out
		return
	}
	
	// Check if request was cancelled
	select {
	case <-req.Context.Done():
		return
	default:
	}
	
	response := &Response{
		Data: data,
	}
	
	// Check for error in response
	if !data.Get("ok").Bool() {
		if errVal := data.Get("error"); !errVal.IsUndefined() {
			response.Error = fmt.Errorf("worker error: %s", errVal.String())
		} else {
			response.Error = fmt.Errorf("unknown worker error")
		}
	}
	
	select {
	case req.Response <- response:
	default:
		// Channel is full or closed, ignore
	}
}

// Close closes the queue and cancels all pending requests
func (q *Queue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	
	if q.closed {
		return nil
	}
	
	q.closed = true
	
	// Cancel all pending requests
	for _, req := range q.requests {
		req.Cancel()
		close(req.Response)
	}
	
	q.requests = make(map[int64]*Request)
	
	// Terminate the Worker
	if !q.worker.IsNull() {
		q.worker.Call("terminate")
	}
	
	return nil
}

// IsHealthy checks if the queue and Worker are in a healthy state
func (q *Queue) IsHealthy() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	
	return !q.closed && !q.worker.IsNull()
}