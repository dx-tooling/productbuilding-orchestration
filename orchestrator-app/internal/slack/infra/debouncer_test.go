package infra

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDebouncer_Debounce(t *testing.T) {
	d := NewDebouncer()

	var counter int32
	var wg sync.WaitGroup
	wg.Add(1)

	// Call debounce multiple times rapidly
	key := "test-key"
	for i := 0; i < 5; i++ {
		d.Debounce(key, 100*time.Millisecond, func() {
			atomic.AddInt32(&counter, 1)
			wg.Done()
		})
	}

	// Wait for the debounced function to execute
	wg.Wait()

	// Should only execute once despite 5 rapid calls
	if atomic.LoadInt32(&counter) != 1 {
		t.Errorf("Expected function to execute 1 time, got %d", counter)
	}
}

func TestDebouncer_Debounce_DifferentKeys(t *testing.T) {
	d := NewDebouncer()

	var counter1, counter2 int32
	var wg sync.WaitGroup
	wg.Add(2)

	// Two different keys should execute independently
	d.Debounce("key1", 50*time.Millisecond, func() {
		atomic.AddInt32(&counter1, 1)
		wg.Done()
	})

	d.Debounce("key2", 50*time.Millisecond, func() {
		atomic.AddInt32(&counter2, 1)
		wg.Done()
	})

	wg.Wait()

	if atomic.LoadInt32(&counter1) != 1 {
		t.Errorf("Expected key1 to execute 1 time, got %d", counter1)
	}
	if atomic.LoadInt32(&counter2) != 1 {
		t.Errorf("Expected key2 to execute 1 time, got %d", counter2)
	}
}

func TestDebouncer_Debounce_Reset(t *testing.T) {
	d := NewDebouncer()

	var counter int32
	var mu sync.Mutex
	executed := false

	key := "reset-key"

	// First call
	d.Debounce(key, 100*time.Millisecond, func() {
		mu.Lock()
		executed = true
		mu.Unlock()
		atomic.AddInt32(&counter, 1)
	})

	// Wait half the debounce period
	time.Sleep(60 * time.Millisecond)

	// Reset by calling again - this should reset the timer
	d.Debounce(key, 100*time.Millisecond, func() {
		mu.Lock()
		executed = true
		mu.Unlock()
		atomic.AddInt32(&counter, 1)
	})

	// Wait for the reset debounce to complete (should be ~160ms total)
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	if !executed {
		t.Error("Function should have executed after reset")
	}
	mu.Unlock()

	// Should still only execute once
	if atomic.LoadInt32(&counter) != 1 {
		t.Errorf("Expected function to execute 1 time after reset, got %d", counter)
	}
}

func TestDebouncer_ConcurrentAccess(t *testing.T) {
	d := NewDebouncer()

	var counter int32
	var wg sync.WaitGroup

	// Multiple goroutines calling debounce on same key
	key := "concurrent-key"
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.Debounce(key, 50*time.Millisecond, func() {
				atomic.AddInt32(&counter, 1)
			})
			time.Sleep(10 * time.Millisecond) // Small delay between calls
		}()
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond) // Wait for debounce to fire

	// Should execute at most a few times (due to race conditions, might be 1-3)
	// But definitely not 10
	finalCount := atomic.LoadInt32(&counter)
	if finalCount > 3 {
		t.Errorf("Expected at most 3 executions with concurrent access, got %d", finalCount)
	}
}

func TestDebouncer_Cleanup(t *testing.T) {
	d := NewDebouncer()

	var counter int32

	// Execute debounce
	d.Debounce("cleanup-key", 50*time.Millisecond, func() {
		atomic.AddInt32(&counter, 1)
	})

	// Wait for execution
	time.Sleep(100 * time.Millisecond)

	// Timer should be cleaned up after execution
	// Next debounce should work normally
	d.Debounce("cleanup-key", 50*time.Millisecond, func() {
		atomic.AddInt32(&counter, 1)
	})

	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&counter) != 2 {
		t.Errorf("Expected 2 executions after cleanup, got %d", counter)
	}
}
