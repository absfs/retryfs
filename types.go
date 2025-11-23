package retryfs

import (
	"time"
)

// ErrorClass represents the classification of an error
type ErrorClass int

const (
	// ErrorUnknown means we can't determine if the error is retryable
	ErrorUnknown ErrorClass = iota
	// ErrorRetryable means the error is transient and may succeed on retry
	ErrorRetryable
	// ErrorPermanent means the error is permanent and won't change on retry
	ErrorPermanent
)

// String returns a string representation of the ErrorClass
func (ec ErrorClass) String() string {
	switch ec {
	case ErrorUnknown:
		return "unknown"
	case ErrorRetryable:
		return "retryable"
	case ErrorPermanent:
		return "permanent"
	default:
		return "invalid"
	}
}

// Operation represents a filesystem operation type
type Operation string

const (
	OpOpen     Operation = "open"
	OpCreate   Operation = "create"
	OpOpenFile Operation = "openfile"
	OpStat     Operation = "stat"
	OpLstat    Operation = "lstat"
	OpReadDir  Operation = "readdir"
	OpMkdirAll Operation = "mkdirall"
	OpRemove   Operation = "remove"
	OpRename   Operation = "rename"
	OpSymlink  Operation = "symlink"
	OpReadlink Operation = "readlink"
	OpChmod    Operation = "chmod"
	OpLchown   Operation = "lchown"
	OpChown    Operation = "chown"
	OpChtimes  Operation = "chtimes"
	OpChroot   Operation = "chroot"
	OpRoot     Operation = "root"
	OpJoin     Operation = "join"
	OpTempFile Operation = "tempfile"
)

// Policy defines the retry behavior
type Policy struct {
	// MaxAttempts is the maximum number of attempts (including the first try)
	MaxAttempts int
	// BaseDelay is the initial delay before the first retry
	BaseDelay time.Duration
	// MaxDelay is the maximum delay between retries
	MaxDelay time.Duration
	// Jitter is the random variance factor (0.0 to 1.0) added to delay
	// A jitter of 0.25 means ±25% randomness
	Jitter float64
	// Multiplier is the exponential backoff multiplier (usually 2.0)
	Multiplier float64
}

// DefaultPolicy provides sensible defaults for retry behavior
var DefaultPolicy = &Policy{
	MaxAttempts: 5,
	BaseDelay:   100 * time.Millisecond,
	MaxDelay:    30 * time.Second,
	Jitter:      0.25,
	Multiplier:  2.0,
}

// ErrorClassifier is a function that classifies an error
type ErrorClassifier func(err error) ErrorClass

// Config holds the configuration for RetryFS
type Config struct {
	// DefaultPolicy is the policy used when no operation-specific policy exists
	DefaultPolicy *Policy
	// OperationPolicies maps operations to their specific retry policies
	OperationPolicies map[Operation]*Policy
	// ErrorClassifier classifies errors as retryable or permanent
	ErrorClassifier ErrorClassifier
	// OnRetry is called before each retry attempt
	OnRetry func(op Operation, attempt int, err error)
}

// DefaultConfig returns a new Config with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		DefaultPolicy:     DefaultPolicy,
		OperationPolicies: make(map[Operation]*Policy),
		ErrorClassifier:   defaultErrorClassifier,
	}
}

// GetPolicy returns the policy for a given operation
func (c *Config) GetPolicy(op Operation) *Policy {
	if policy, ok := c.OperationPolicies[op]; ok {
		return policy
	}
	return c.DefaultPolicy
}

// Metrics holds statistics about retry behavior
type Metrics struct {
	// TotalAttempts is the total number of attempts across all operations
	TotalAttempts int64
	// TotalRetries is the total number of retries (attempts - initial tries)
	TotalRetries int64
	// TotalSuccesses is the total number of successful operations
	TotalSuccesses int64
	// TotalFailures is the total number of failed operations (after all retries)
	TotalFailures int64
	// ErrorsByClass tracks errors by their classification
	ErrorsByClass map[ErrorClass]int64
	// OperationMetrics tracks metrics per operation type
	OperationMetrics map[Operation]*OperationMetrics
}

// OperationMetrics holds metrics for a specific operation type
type OperationMetrics struct {
	Attempts  int64
	Retries   int64
	Successes int64
	Failures  int64
}

// NewMetrics creates a new Metrics instance
func NewMetrics() *Metrics {
	return &Metrics{
		ErrorsByClass:    make(map[ErrorClass]int64),
		OperationMetrics: make(map[Operation]*OperationMetrics),
	}
}

// RecordAttempt records an attempt for an operation
func (m *Metrics) RecordAttempt(op Operation, isRetry bool) {
	m.TotalAttempts++
	if isRetry {
		m.TotalRetries++
	}

	if _, ok := m.OperationMetrics[op]; !ok {
		m.OperationMetrics[op] = &OperationMetrics{}
	}
	om := m.OperationMetrics[op]
	om.Attempts++
	if isRetry {
		om.Retries++
	}
}

// RecordSuccess records a successful operation
func (m *Metrics) RecordSuccess(op Operation) {
	m.TotalSuccesses++
	if om, ok := m.OperationMetrics[op]; ok {
		om.Successes++
	}
}

// RecordFailure records a failed operation
func (m *Metrics) RecordFailure(op Operation, errClass ErrorClass) {
	m.TotalFailures++
	m.ErrorsByClass[errClass]++
	if om, ok := m.OperationMetrics[op]; ok {
		om.Failures++
	}
}
