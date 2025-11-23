package retryfs

import (
	"sync"
	"time"
)

// PerOperationCircuitBreaker manages circuit breakers for individual operations
type PerOperationCircuitBreaker struct {
	breakers map[Operation]*CircuitBreaker
	mu       sync.RWMutex
	config   *CircuitBreakerConfig
}

// CircuitBreakerConfig holds configuration for creating new circuit breakers
type CircuitBreakerConfig struct {
	FailureThreshold int
	SuccessThreshold int
	Timeout          time.Duration
	OnStateChange    func(op Operation, from, to State)
}

// NewPerOperationCircuitBreaker creates a new per-operation circuit breaker manager
func NewPerOperationCircuitBreaker(config *CircuitBreakerConfig) *PerOperationCircuitBreaker {
	if config == nil {
		config = &CircuitBreakerConfig{
			FailureThreshold: 5,
			SuccessThreshold: 2,
			Timeout:          60 * time.Second,
		}
	}

	return &PerOperationCircuitBreaker{
		breakers: make(map[Operation]*CircuitBreaker),
		config:   config,
	}
}

// GetCircuitBreaker returns the circuit breaker for a specific operation
// Creates one if it doesn't exist
func (pocb *PerOperationCircuitBreaker) GetCircuitBreaker(op Operation) *CircuitBreaker {
	pocb.mu.RLock()
	cb, exists := pocb.breakers[op]
	pocb.mu.RUnlock()

	if exists {
		return cb
	}

	// Create new circuit breaker
	pocb.mu.Lock()
	defer pocb.mu.Unlock()

	// Double-check after acquiring write lock
	if cb, exists := pocb.breakers[op]; exists {
		return cb
	}

	cb = &CircuitBreaker{
		FailureThreshold: pocb.config.FailureThreshold,
		SuccessThreshold: pocb.config.SuccessThreshold,
		Timeout:          pocb.config.Timeout,
		state:            StateClosed,
	}

	// Wrap the OnStateChange callback to include operation
	if pocb.config.OnStateChange != nil {
		cb.OnStateChange = func(from, to State) {
			pocb.config.OnStateChange(op, from, to)
		}
	}

	pocb.breakers[op] = cb
	return cb
}

// Call executes a function with the circuit breaker for the given operation
func (pocb *PerOperationCircuitBreaker) Call(op Operation, fn func() error) error {
	cb := pocb.GetCircuitBreaker(op)
	return cb.Call(fn)
}

// GetAllStates returns the state of all circuit breakers
func (pocb *PerOperationCircuitBreaker) GetAllStates() map[Operation]State {
	pocb.mu.RLock()
	defer pocb.mu.RUnlock()

	states := make(map[Operation]State, len(pocb.breakers))
	for op, cb := range pocb.breakers {
		states[op] = cb.GetState()
	}
	return states
}

// GetAllStats returns statistics for all circuit breakers
func (pocb *PerOperationCircuitBreaker) GetAllStats() map[Operation]CircuitBreakerStats {
	pocb.mu.RLock()
	defer pocb.mu.RUnlock()

	stats := make(map[Operation]CircuitBreakerStats, len(pocb.breakers))
	for op, cb := range pocb.breakers {
		stats[op] = cb.GetStats()
	}
	return stats
}

// Reset resets all circuit breakers
func (pocb *PerOperationCircuitBreaker) Reset() {
	pocb.mu.RLock()
	defer pocb.mu.RUnlock()

	for _, cb := range pocb.breakers {
		cb.Reset()
	}
}

// ResetOperation resets the circuit breaker for a specific operation
func (pocb *PerOperationCircuitBreaker) ResetOperation(op Operation) {
	pocb.mu.RLock()
	cb, exists := pocb.breakers[op]
	pocb.mu.RUnlock()

	if exists {
		cb.Reset()
	}
}
