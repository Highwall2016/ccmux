// Package relay handles the WebSocket connection to the ccmux backend.
package relay

import (
	"sync"
	"time"
)

const batchInterval = 16 * time.Millisecond

// FlushFunc is called when the batcher flushes accumulated output for a session.
type FlushFunc func(sessionID string, data []byte)

// Batcher accumulates PTY output per session and flushes it every 16 ms.
// This reduces packet count by coalescing rapid output into single frames.
type Batcher struct {
	mu       sync.Mutex
	pending  map[string][]byte
	flush    FlushFunc
	ticker   *time.Ticker
	stopCh   chan struct{}
}

// NewBatcher creates and starts a Batcher.
func NewBatcher(flush FlushFunc) *Batcher {
	b := &Batcher{
		pending: make(map[string][]byte),
		flush:   flush,
		ticker:  time.NewTicker(batchInterval),
		stopCh:  make(chan struct{}),
	}
	go b.run()
	return b
}

// Add appends output data for a session to the pending buffer.
func (b *Batcher) Add(sessionID string, data []byte) {
	b.mu.Lock()
	b.pending[sessionID] = append(b.pending[sessionID], data...)
	b.mu.Unlock()
}

// Stop halts the batcher, flushing any remaining data.
func (b *Batcher) Stop() {
	b.ticker.Stop()
	close(b.stopCh)
}

func (b *Batcher) run() {
	for {
		select {
		case <-b.ticker.C:
			b.flushAll()
		case <-b.stopCh:
			b.flushAll()
			return
		}
	}
}

func (b *Batcher) flushAll() {
	b.mu.Lock()
	if len(b.pending) == 0 {
		b.mu.Unlock()
		return
	}
	// Snapshot and clear the pending map under lock.
	snapshot := b.pending
	b.pending = make(map[string][]byte)
	b.mu.Unlock()

	for id, data := range snapshot {
		b.flush(id, data)
	}
}
