package retryfs

import (
	"errors"
	"sync"
	"testing"
	"time"

)



func TestNewCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker()

	if cb.FailureThreshold != 5 {
		t.Errorf("Expected FailureThreshold=5, got %d", cb.FailureThreshold)
	}
	if cb.SuccessThreshold != 2 {
		t.Errorf("Expected SuccessThreshold=2, got %d", cb.SuccessThreshold)
	}
	if cb.Timeout != 60*time.Second {
		t.Errorf("Expected Timeout=60s, got %v", cb.Timeout)
	}
	if cb.GetState() != StateClosed {
		t.Errorf("Expected initial state=Closed, got %v", cb.GetState())
	}
}

func TestCircuitBreakerClosedToOpen(t *testing.T) {
	cb := &CircuitBreaker{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
	}

	testErr := errors.New("test error")

	// First 2 failures should keep circuit closed
	for i := 0; i < 2; i++ {
		err := cb.Call(func() error { return testErr })
		if err != testErr {
			t.Errorf("Expected error to be returned, got %v", err)
		}
		if cb.GetState() != StateClosed {
			t.Errorf("Circuit should still be closed after %d failures", i+1)
		}
	}

	// Third failure should open the circuit
	err := cb.Call(func() error { return testErr })
	if err != testErr {
		t.Errorf("Expected error to be returned, got %v", err)
	}
	if cb.GetState() != StateOpen {
		t.Errorf("Circuit should be open after %d failures", cb.FailureThreshold)
	}

	// Next call should fail fast with ErrCircuitOpen
	err = cb.Call(func() error { return nil })
	if err != ErrCircuitOpen {
		t.Errorf("Expected ErrCircuitOpen, got %v", err)
	}
}

func TestCircuitBreakerOpenToHalfOpen(t *testing.T) {
	cb := &CircuitBreaker{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
	}

	testErr := errors.New("test error")

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Call(func() error { return testErr })
	}
	if cb.GetState() != StateOpen {
		t.Fatal("Circuit should be open")
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Next call should transition to half-open
	called := false
	cb.Call(func() error {
		called = true
		return nil
	})

	if !called {
		t.Error("Function should have been called in half-open state")
	}
}

func TestCircuitBreakerHalfOpenToClosed(t *testing.T) {
	cb := &CircuitBreaker{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
	}

	testErr := errors.New("test error")

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Call(func() error { return testErr })
	}
	if cb.GetState() != StateOpen {
		t.Fatal("Circuit should be open")
	}

	// Wait for timeout to transition to half-open
	time.Sleep(60 * time.Millisecond)

	// Two successful calls should close the circuit
	for i := 0; i < 2; i++ {
		err := cb.Call(func() error { return nil })
		if err != nil {
			t.Errorf("Expected success, got %v", err)
		}
	}

	if cb.GetState() != StateClosed {
		t.Errorf("Circuit should be closed after %d successes, got state %v",
			cb.SuccessThreshold, cb.GetState())
	}
}

func TestCircuitBreakerHalfOpenToOpen(t *testing.T) {
	cb := &CircuitBreaker{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
	}

	testErr := errors.New("test error")

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Call(func() error { return testErr })
	}
	if cb.GetState() != StateOpen {
		t.Fatal("Circuit should be open")
	}

	// Wait for timeout to transition to half-open
	time.Sleep(60 * time.Millisecond)

	// One failure in half-open should return to open
	err := cb.Call(func() error { return testErr })
	if err != testErr {
		t.Errorf("Expected error to be returned, got %v", err)
	}

	if cb.GetState() != StateOpen {
		t.Errorf("Circuit should be open after failure in half-open state, got %v", cb.GetState())
	}
}

func TestCircuitBreakerSuccessResetsFailureCount(t *testing.T) {
	cb := &CircuitBreaker{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
	}

	testErr := errors.New("test error")

	// Fail twice
	for i := 0; i < 2; i++ {
		cb.Call(func() error { return testErr })
	}

	// Succeed once - should reset failure count
	cb.Call(func() error { return nil })

	if cb.GetState() != StateClosed {
		t.Error("Circuit should still be closed after success")
	}

	// Two more failures shouldn't open (count was reset)
	for i := 0; i < 2; i++ {
		cb.Call(func() error { return testErr })
	}

	if cb.GetState() != StateClosed {
		t.Error("Circuit should still be closed (failure count was reset)")
	}

	// One more failure should now open it
	cb.Call(func() error { return testErr })

	if cb.GetState() != StateOpen {
		t.Error("Circuit should be open after reaching threshold")
	}
}

func TestCircuitBreakerStateChangeCallback(t *testing.T) {
	var mu sync.Mutex
	var transitions []string
	cb := &CircuitBreaker{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          50 * time.Millisecond,
		OnStateChange: func(from, to State) {
			mu.Lock()
			transitions = append(transitions, from.String()+"->"+to.String())
			mu.Unlock()
		},
	}

	testErr := errors.New("test error")

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Call(func() error { return testErr })
	}

	// Give callback time to run (it runs in goroutine)
	time.Sleep(10 * time.Millisecond)

	// Wait for timeout and succeed to close
	time.Sleep(60 * time.Millisecond)
	cb.Call(func() error { return nil })

	time.Sleep(10 * time.Millisecond)

	// Verify transitions
	mu.Lock()
	transCount := len(transitions)
	var transCopy []string
	transCopy = append(transCopy, transitions...)
	mu.Unlock()

	if transCount < 2 {
		t.Errorf("Expected at least 2 transitions, got %d: %v", transCount, transCopy)
	}

	// Should have seen closed->open and half-open->closed (or open->half-open)
	hasClosedToOpen := false
	for _, trans := range transCopy {
		if trans == "closed->open" {
			hasClosedToOpen = true
		}
	}

	if !hasClosedToOpen {
		t.Errorf("Expected to see closed->open transition, got %v", transCopy)
	}
}

func TestCircuitBreakerReset(t *testing.T) {
	cb := &CircuitBreaker{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
	}

	testErr := errors.New("test error")

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Call(func() error { return testErr })
	}
	if cb.GetState() != StateOpen {
		t.Fatal("Circuit should be open")
	}

	// Reset should close the circuit
	cb.Reset()

	if cb.GetState() != StateClosed {
		t.Error("Reset should close the circuit")
	}

	stats := cb.GetStats()
	if stats.ConsecutiveErrors != 0 {
		t.Errorf("Reset should clear consecutive errors, got %d", stats.ConsecutiveErrors)
	}

	// Should accept calls normally now
	err := cb.Call(func() error { return nil })
	if err != nil {
		t.Errorf("Expected success after reset, got %v", err)
	}
}

func TestCircuitBreakerGetStats(t *testing.T) {
	cb := &CircuitBreaker{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
	}

	testErr := errors.New("test error")

	// Initial stats
	stats := cb.GetStats()
	if stats.State != StateClosed {
		t.Error("Initial state should be closed")
	}
	if stats.ConsecutiveErrors != 0 {
		t.Error("Initial consecutive errors should be 0")
	}

	// Fail twice
	before := time.Now()
	for i := 0; i < 2; i++ {
		cb.Call(func() error { return testErr })
	}

	stats = cb.GetStats()
	if stats.ConsecutiveErrors != 2 {
		t.Errorf("Expected 2 consecutive errors, got %d", stats.ConsecutiveErrors)
	}
	if stats.LastFailureTime.Before(before) {
		t.Error("LastFailureTime should be updated")
	}

	// Succeed once
	cb.Call(func() error { return nil })

	stats = cb.GetStats()
	if stats.ConsecutiveErrors != 0 {
		t.Error("Success should reset consecutive errors")
	}
	if stats.ConsecutiveSuccess != 0 {
		t.Error("ConsecutiveSuccess should only count in half-open state")
	}
}

func TestStateString(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{State(999), "unknown"},
	}

	for _, tt := range tests {
		result := tt.state.String()
		if result != tt.expected {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, result, tt.expected)
		}
	}
}

func TestCircuitBreakerWithRetryFS(t *testing.T) {
	mock := &mockFailingFS{
		FileSystem:   mustNewMemFS(),
		failuresLeft: 100, // Always fail
	}

	cb := &CircuitBreaker{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
	}

	policy := &Policy{
		MaxAttempts: 2,
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    10 * time.Millisecond,
		Jitter:      0,
		Multiplier:  2.0,
	}

	rfs := New(mock, WithPolicy(policy), WithCircuitBreaker(cb)).(*RetryFS)

	// First call should fail after retries
	err := rfs.MkdirAll("/test1", 0755)
	if err == nil {
		t.Error("Expected error")
	}
	if cb.GetState() != StateClosed {
		t.Error("Circuit should still be closed after first failure")
	}

	// Second call should fail
	err = rfs.MkdirAll("/test2", 0755)
	if err == nil {
		t.Error("Expected error")
	}

	// Third call should fail and open circuit
	err = rfs.MkdirAll("/test3", 0755)
	if err == nil {
		t.Error("Expected error")
	}

	if cb.GetState() != StateOpen {
		t.Errorf("Circuit should be open, got %v", cb.GetState())
	}

	// Fourth call should fail fast
	err = rfs.MkdirAll("/test4", 0755)
	if err != ErrCircuitOpen {
		t.Errorf("Expected ErrCircuitOpen, got %v", err)
	}

	// Verify circuit breaker prevented retries
	metrics := rfs.GetMetrics()
	// The fourth call shouldn't have attempted retries
	if metrics.TotalAttempts >= 8 {
		t.Errorf("Circuit breaker should have prevented some attempts, got %d", metrics.TotalAttempts)
	}
}
