package infra

import (
	"sync"
	"time"
)

// Debouncer batches rapid function calls and executes them once after a delay
type Debouncer struct {
	timers map[string]*time.Timer
	mu     sync.Mutex
}

// NewDebouncer creates a new debouncer instance
func NewDebouncer() *Debouncer {
	return &Debouncer{
		timers: make(map[string]*time.Timer),
	}
}

// Debounce schedules a function to be called after the specified duration.
// If the same key is debounced again before the duration expires,
// the previous timer is cancelled and reset.
func (d *Debouncer) Debounce(key string, wait time.Duration, fn func()) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Cancel existing timer for this key
	if existing, ok := d.timers[key]; ok {
		existing.Stop()
	}

	// Create new timer
	timer := time.AfterFunc(wait, func() {
		d.mu.Lock()
		delete(d.timers, key)
		d.mu.Unlock()

		fn()
	})

	d.timers[key] = timer
}
