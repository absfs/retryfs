package retryfs

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPerOperationCircuitBreaker_Creation(t *testing.T) {
	config := &CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 1,
		Timeout:          10 * time.Second,
	}

	pocb := NewPerOperationCircuitBreaker(config)

	if pocb == nil {
		t.Fatal("Expected non-nil PerOperationCircuitBreaker")
	}

	if pocb.config.FailureThreshold != 3 {
		t.Errorf("Expected FailureThreshold=3, got: %d", pocb.config.FailureThreshold)
	}
}

func TestPerOperationCircuitBreaker_DefaultConfig(t *testing.T) {
	pocb := NewPerOperationCircuitBreaker(nil)

	if pocb.config.FailureThreshold != 5 {
		t.Errorf("Expected default FailureThreshold=5, got: %d", pocb.config.FailureThreshold)
	}

	if pocb.config.Timeout != 60*time.Second {
		t.Errorf("Expected default Timeout=60s, got: %v", pocb.config.Timeout)
	}
}

func TestPerOperationCircuitBreaker_GetCircuitBreaker(t *testing.T) {
	pocb := NewPerOperationCircuitBreaker(nil)

	cb1 := pocb.GetCircuitBreaker(OpOpen)
	cb2 := pocb.GetCircuitBreaker(OpOpen)

	// Should return the same instance
	if cb1 != cb2 {
		t.Error("Expected same circuit breaker instance for same operation")
	}

	cbCreate := pocb.GetCircuitBreaker(OpCreate)
	if cb1 == cbCreate {
		t.Error("Expected different circuit breaker instances for different operations")
	}
}

func TestPerOperationCircuitBreaker_IndependentStates(t *testing.T) {
	pocb := NewPerOperationCircuitBreaker(&CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          100 * time.Millisecond,
	})

	// Fail OpOpen circuit breaker
	err := errors.New("test error")
	for i := 0; i < 3; i++ {
		pocb.Call(OpOpen, func() error { return err })
	}

	// OpOpen should be open
	cbOpen := pocb.GetCircuitBreaker(OpOpen)
	if cbOpen.GetState() != StateOpen {
		t.Errorf("Expected OpOpen circuit to be open, got: %s", cbOpen.GetState())
	}

	// OpCreate should still be closed
	cbCreate := pocb.GetCircuitBreaker(OpCreate)
	if cbCreate.GetState() != StateClosed {
		t.Errorf("Expected OpCreate circuit to be closed, got: %s", cbCreate.GetState())
	}

	// OpCreate should still work
	err = pocb.Call(OpCreate, func() error { return nil })
	if err != nil {
		t.Errorf("Expected OpCreate to succeed, got error: %v", err)
	}
}

func TestPerOperationCircuitBreaker_Call(t *testing.T) {
	pocb := NewPerOperationCircuitBreaker(&CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          50 * time.Millisecond,
	})

	// Success case
	err := pocb.Call(OpOpen, func() error { return nil })
	if err != nil {
		t.Errorf("Expected success, got error: %v", err)
	}

	// Failure case
	testErr := errors.New("test error")
	err = pocb.Call(OpOpen, func() error { return testErr })
	if err != testErr {
		t.Errorf("Expected test error, got: %v", err)
	}

	// Open circuit
	for i := 0; i < 3; i++ {
		pocb.Call(OpOpen, func() error { return testErr })
	}

	// Should fail with circuit open error
	err = pocb.Call(OpOpen, func() error { return nil })
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("Expected ErrCircuitOpen, got: %v", err)
	}
}

func TestPerOperationCircuitBreaker_GetAllStates(t *testing.T) {
	pocb := NewPerOperationCircuitBreaker(&CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          100 * time.Millisecond,
	})

	// Create circuit breakers for multiple operations
	pocb.Call(OpOpen, func() error { return nil })
	pocb.Call(OpCreate, func() error { return nil })
	pocb.Call(OpStat, func() error { return nil })

	states := pocb.GetAllStates()

	if len(states) != 3 {
		t.Errorf("Expected 3 circuit breakers, got: %d", len(states))
	}

	if states[OpOpen] != StateClosed {
		t.Errorf("Expected OpOpen to be closed, got: %s", states[OpOpen])
	}
}

func TestPerOperationCircuitBreaker_GetAllStats(t *testing.T) {
	pocb := NewPerOperationCircuitBreaker(&CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          100 * time.Millisecond,
	})

	// Perform some operations
	pocb.Call(OpOpen, func() error { return nil })
	pocb.Call(OpOpen, func() error { return errors.New("error") })

	stats := pocb.GetAllStats()

	if len(stats) != 1 {
		t.Errorf("Expected 1 circuit breaker stats, got: %d", len(stats))
	}

	openStats, exists := stats[OpOpen]
	if !exists {
		t.Fatal("Expected stats for OpOpen")
	}

	if openStats.State != StateClosed {
		t.Errorf("Expected state to be closed, got: %s", openStats.State)
	}
}

func TestPerOperationCircuitBreaker_Reset(t *testing.T) {
	pocb := NewPerOperationCircuitBreaker(&CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          100 * time.Millisecond,
	})

	// Open multiple circuits
	testErr := errors.New("test error")
	for i := 0; i < 3; i++ {
		pocb.Call(OpOpen, func() error { return testErr })
		pocb.Call(OpCreate, func() error { return testErr })
	}

	// Both should be open
	if pocb.GetCircuitBreaker(OpOpen).GetState() != StateOpen {
		t.Error("Expected OpOpen to be open")
	}
	if pocb.GetCircuitBreaker(OpCreate).GetState() != StateOpen {
		t.Error("Expected OpCreate to be open")
	}

	// Reset all
	pocb.Reset()

	// Both should be closed
	if pocb.GetCircuitBreaker(OpOpen).GetState() != StateClosed {
		t.Error("Expected OpOpen to be closed after reset")
	}
	if pocb.GetCircuitBreaker(OpCreate).GetState() != StateClosed {
		t.Error("Expected OpCreate to be closed after reset")
	}
}

func TestPerOperationCircuitBreaker_ResetOperation(t *testing.T) {
	pocb := NewPerOperationCircuitBreaker(&CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          100 * time.Millisecond,
	})

	// Open multiple circuits
	testErr := errors.New("test error")
	for i := 0; i < 3; i++ {
		pocb.Call(OpOpen, func() error { return testErr })
		pocb.Call(OpCreate, func() error { return testErr })
	}

	// Reset only OpOpen
	pocb.ResetOperation(OpOpen)

	// OpOpen should be closed, OpCreate still open
	if pocb.GetCircuitBreaker(OpOpen).GetState() != StateClosed {
		t.Error("Expected OpOpen to be closed after reset")
	}
	if pocb.GetCircuitBreaker(OpCreate).GetState() != StateOpen {
		t.Error("Expected OpCreate to still be open")
	}
}

func TestPerOperationCircuitBreaker_OnStateChange(t *testing.T) {
	var stateChanges int32
	var mu sync.Mutex
	var lastOp Operation

	pocb := NewPerOperationCircuitBreaker(&CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          50 * time.Millisecond,
		OnStateChange: func(op Operation, from, to State) {
			atomic.AddInt32(&stateChanges, 1)
			mu.Lock()
			lastOp = op
			mu.Unlock()
		},
	})

	// Cause failures to trigger state change
	testErr := errors.New("test error")
	for i := 0; i < 3; i++ {
		pocb.Call(OpOpen, func() error { return testErr })
	}

	// Give callback time to execute
	time.Sleep(20 * time.Millisecond)

	changes := atomic.LoadInt32(&stateChanges)
	if changes == 0 {
		t.Error("Expected state change callback to be called")
	}

	mu.Lock()
	opCopy := lastOp
	mu.Unlock()

	if opCopy != OpOpen {
		t.Errorf("Expected last operation to be OpOpen, got: %s", opCopy)
	}
}

func TestPerOperationCircuitBreaker_ThreadSafety(t *testing.T) {
	pocb := NewPerOperationCircuitBreaker(&CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          10 * time.Millisecond,
	})

	var wg sync.WaitGroup
	operations := []Operation{OpOpen, OpCreate, OpStat, OpMkdirAll}

	// Spawn multiple goroutines calling different operations
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			op := operations[id%len(operations)]
			for j := 0; j < 100; j++ {
				pocb.Call(op, func() error {
					if j%3 == 0 {
						return errors.New("random error")
					}
					return nil
				})
			}
		}(i)
	}

	wg.Wait()

	// Verify we have circuit breakers for operations
	states := pocb.GetAllStates()
	if len(states) == 0 {
		t.Error("Expected some circuit breakers to be created")
	}

	t.Logf("Created %d circuit breakers", len(states))
}
