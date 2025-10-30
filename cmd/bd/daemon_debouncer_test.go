package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDebouncer_BatchesMultipleTriggers(t *testing.T) {
	var count int32
	debouncer := NewDebouncer(50*time.Millisecond, func() {
		atomic.AddInt32(&count, 1)
	})
	t.Cleanup(debouncer.Cancel)

	debouncer.Trigger()
	debouncer.Trigger()
	debouncer.Trigger()

	time.Sleep(30 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 0 {
		t.Errorf("action fired too early: got %d, want 0", got)
	}

	time.Sleep(40 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Errorf("action should have fired once: got %d, want 1", got)
	}
}

func TestDebouncer_ResetsTimerOnSubsequentTriggers(t *testing.T) {
	var count int32
	debouncer := NewDebouncer(50*time.Millisecond, func() {
		atomic.AddInt32(&count, 1)
	})
	t.Cleanup(debouncer.Cancel)

	debouncer.Trigger()
	time.Sleep(30 * time.Millisecond)

	debouncer.Trigger()
	time.Sleep(30 * time.Millisecond)

	if got := atomic.LoadInt32(&count); got != 0 {
		t.Errorf("action fired too early after timer reset: got %d, want 0", got)
	}

	time.Sleep(30 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Errorf("action should have fired once after final timer: got %d, want 1", got)
	}
}

func TestDebouncer_CancelDuringWait(t *testing.T) {
	var count int32
	debouncer := NewDebouncer(50*time.Millisecond, func() {
		atomic.AddInt32(&count, 1)
	})
	t.Cleanup(debouncer.Cancel)

	debouncer.Trigger()
	time.Sleep(20 * time.Millisecond)

	debouncer.Cancel()

	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 0 {
		t.Errorf("action should not have fired after cancel: got %d, want 0", got)
	}
}

func TestDebouncer_CancelWithNoPendingAction(t *testing.T) {
	var count int32
	debouncer := NewDebouncer(50*time.Millisecond, func() {
		atomic.AddInt32(&count, 1)
	})
	t.Cleanup(debouncer.Cancel)

	debouncer.Cancel()

	debouncer.Trigger()
	time.Sleep(70 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Errorf("action should fire normally after cancel with no pending action: got %d, want 1", got)
	}
}

func TestDebouncer_ThreadSafety(t *testing.T) {
	var count int32
	debouncer := NewDebouncer(50*time.Millisecond, func() {
		atomic.AddInt32(&count, 1)
	})
	t.Cleanup(debouncer.Cancel)

	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			debouncer.Trigger()
		}()
	}

	close(start)
	wg.Wait()

	time.Sleep(100 * time.Millisecond)

	got := atomic.LoadInt32(&count)
	if got != 1 {
		t.Errorf("all concurrent triggers should batch to exactly 1 action: got %d, want 1", got)
	}
}

func TestDebouncer_ConcurrentCancelAndTrigger(t *testing.T) {
	var count int32
	debouncer := NewDebouncer(50*time.Millisecond, func() {
		atomic.AddInt32(&count, 1)
	})
	t.Cleanup(debouncer.Cancel)

	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			if index%2 == 0 {
				debouncer.Trigger()
			} else {
				debouncer.Cancel()
			}
		}(i)
	}

	wg.Wait()
	debouncer.Cancel()

	time.Sleep(100 * time.Millisecond)

	got := atomic.LoadInt32(&count)
	if got != 0 && got != 1 {
		t.Errorf("unexpected action count with concurrent cancel/trigger: got %d, want 0 or 1", got)
	}
}

func TestDebouncer_MultipleSequentialTriggerCycles(t *testing.T) {
	var count int32
	debouncer := NewDebouncer(30*time.Millisecond, func() {
		atomic.AddInt32(&count, 1)
	})
	t.Cleanup(debouncer.Cancel)

	debouncer.Trigger()
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Errorf("first cycle: got %d, want 1", got)
	}

	debouncer.Trigger()
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 2 {
		t.Errorf("second cycle: got %d, want 2", got)
	}

	debouncer.Trigger()
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 3 {
		t.Errorf("third cycle: got %d, want 3", got)
	}
}

func TestDebouncer_CancelImmediatelyAfterTrigger(t *testing.T) {
	var count int32
	debouncer := NewDebouncer(50*time.Millisecond, func() {
		atomic.AddInt32(&count, 1)
	})
	t.Cleanup(debouncer.Cancel)

	debouncer.Trigger()
	debouncer.Cancel()

	time.Sleep(80 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 0 {
		t.Errorf("action should not fire after immediate cancel: got %d, want 0", got)
	}
}
