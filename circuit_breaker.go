package retryfs

import (
	"errors"
	"sync"
	"time"
)

// State represents the circuit breaker state
type State int

const (
	// StateClosed means the circuit is closed (normal operation)
	StateClosed State = iota
	// StateOpen means the circuit is open (failing fast)
	StateOpen
	// StateHalfOpen means the circuit is testing if the backend has recovered
	StateHalfOpen
)

// String returns a string representation of the State
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

var (
	// ErrCircuitOpen is returned when the circuit breaker is open
	ErrCircuitOpen = errors.New("circuit breaker is open")
)

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	// FailureThreshold is the number of consecutive failures to open the circuit
	FailureThreshold int
	// SuccessThreshold is the number of consecutive successes to close the circuit from half-open
	SuccessThreshold int
	// Timeout is how long to wait in open state before trying half-open
	Timeout time.Duration
	// OnStateChange is called when the circuit breaker changes state
	OnStateChange func(from, to State)

	mu                sync.RWMutex
	state             State
	consecutiveErrors int
	consecutiveSuccess int
	lastFailureTime   time.Time
}

// NewCircuitBreaker creates a new CircuitBreaker with default settings
func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          60 * time.Second,
		state:            StateClosed,
	}
}

// GetState returns the current state of the circuit breaker
func (cb *CircuitBreaker) GetState() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Call executes the given function with circuit breaker protection
func (cb *CircuitBreaker) Call(fn func() error) error {
	// Check if we should allow the call
	if !cb.allowCall() {
		return ErrCircuitOpen
	}

	// Execute the function
	err := fn()

	// Record the result
	cb.recordResult(err)

	return err
}

// allowCall determines if a call should be allowed
func (cb *CircuitBreaker) allowCall() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		// Always allow calls when closed
		return true

	case StateOpen:
		// Check if timeout has expired
		if time.Since(cb.lastFailureTime) >= cb.Timeout {
			// Transition to half-open
			cb.setState(StateHalfOpen)
			return true
		}
		// Circuit is still open
		return false

	case StateHalfOpen:
		// Allow a single test call in half-open state
		return true

	default:
		return false
	}
}

// recordResult records the result of a call and updates circuit state
func (cb *CircuitBreaker) recordResult(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err == nil {
		// Success
		cb.onSuccess()
	} else {
		// Failure
		cb.onFailure()
	}
}

// onSuccess handles a successful call
func (cb *CircuitBreaker) onSuccess() {
	cb.consecutiveErrors = 0
	cb.consecutiveSuccess++

	switch cb.state {
	case StateHalfOpen:
		// Check if we should close the circuit
		if cb.consecutiveSuccess >= cb.SuccessThreshold {
			cb.setState(StateClosed)
			cb.consecutiveSuccess = 0
		}

	case StateClosed:
		// Already closed, nothing to do
		cb.consecutiveSuccess = 0
	}
}

// onFailure handles a failed call
func (cb *CircuitBreaker) onFailure() {
	cb.consecutiveSuccess = 0
	cb.consecutiveErrors++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case StateClosed:
		// Check if we should open the circuit
		if cb.consecutiveErrors >= cb.FailureThreshold {
			cb.setState(StateOpen)
		}

	case StateHalfOpen:
		// Failure in half-open means backend still not recovered
		cb.setState(StateOpen)
	}
}

// setState transitions the circuit breaker to a new state
func (cb *CircuitBreaker) setState(newState State) {
	if cb.state == newState {
		return
	}

	oldState := cb.state
	cb.state = newState

	// Reset counters on state change
	if newState == StateClosed {
		cb.consecutiveErrors = 0
		cb.consecutiveSuccess = 0
	}

	// Call the state change callback
	if cb.OnStateChange != nil {
		// Call in a goroutine to avoid blocking
		go cb.OnStateChange(oldState, newState)
	}
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.setState(StateClosed)
	cb.consecutiveErrors = 0
	cb.consecutiveSuccess = 0
}

// GetStats returns statistics about the circuit breaker
func (cb *CircuitBreaker) GetStats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return CircuitBreakerStats{
		State:              cb.state,
		ConsecutiveErrors:  cb.consecutiveErrors,
		ConsecutiveSuccess: cb.consecutiveSuccess,
		LastFailureTime:    cb.lastFailureTime,
	}
}

// CircuitBreakerStats contains statistics about a circuit breaker
type CircuitBreakerStats struct {
	State              State
	ConsecutiveErrors  int
	ConsecutiveSuccess int
	LastFailureTime    time.Time
}
