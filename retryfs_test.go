package retryfs

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"syscall"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
)

func TestCalculateBackoff(t *testing.T) {
	policy := &Policy{
		BaseDelay:  100 * time.Millisecond,
		MaxDelay:   5 * time.Second,
		Jitter:     0.0, // No jitter for predictable testing
		Multiplier: 2.0,
	}

	rfs := New(memfs.New()).(*RetryFS)

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 0},                        // No delay on first attempt
		{1, 100 * time.Millisecond},   // 100ms * 2^0
		{2, 200 * time.Millisecond},   // 100ms * 2^1
		{3, 400 * time.Millisecond},   // 100ms * 2^2
		{4, 800 * time.Millisecond},   // 100ms * 2^3
		{5, 1600 * time.Millisecond},  // 100ms * 2^4
		{6, 3200 * time.Millisecond},  // 100ms * 2^5
		{7, 5000 * time.Millisecond},  // Capped at maxDelay
		{10, 5000 * time.Millisecond}, // Still capped
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			delay := rfs.calculateBackoff(policy, tt.attempt)
			if delay != tt.expected {
				t.Errorf("calculateBackoff(attempt=%d) = %v, want %v",
					tt.attempt, delay, tt.expected)
			}
		})
	}
}

func TestCalculateBackoffWithJitter(t *testing.T) {
	policy := &Policy{
		BaseDelay:  100 * time.Millisecond,
		MaxDelay:   10 * time.Second,
		Jitter:     0.25, // ±25% jitter
		Multiplier: 2.0,
	}

	rfs := New(memfs.New()).(*RetryFS)

	// Test that jitter produces values within expected range
	attempt := 3
	baseDelay := 400 * time.Millisecond // 100ms * 2^2
	minDelay := time.Duration(float64(baseDelay) * 0.75)
	maxDelay := time.Duration(float64(baseDelay) * 1.25)

	// Run multiple times to check jitter range
	for i := 0; i < 100; i++ {
		delay := rfs.calculateBackoff(policy, attempt)
		if delay < minDelay || delay > maxDelay {
			t.Errorf("calculateBackoff with jitter produced delay %v, want range [%v, %v]",
				delay, minDelay, maxDelay)
		}
	}
}

func TestShouldRetry(t *testing.T) {
	policy := &Policy{
		MaxAttempts: 3,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    1 * time.Second,
		Multiplier:  2.0,
	}

	rfs := New(memfs.New(), WithPolicy(policy)).(*RetryFS)

	tests := []struct {
		name     string
		err      error
		attempt  int
		expected bool
	}{
		{"nil error", nil, 0, false},
		{"retryable error, attempt 0", syscall.ETIMEDOUT, 0, true},
		{"retryable error, attempt 1", syscall.ETIMEDOUT, 1, true},
		{"retryable error, attempt 2", syscall.ETIMEDOUT, 2, true},
		{"retryable error, max attempts", syscall.ETIMEDOUT, 3, false},
		{"permanent error", fs.ErrNotExist, 0, false},
		{"permanent error, attempt 1", fs.ErrNotExist, 1, false},
		{"unknown error, attempt 0", errors.New("unknown"), 0, true},
		{"unknown error, max attempts", errors.New("unknown"), 3, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rfs.shouldRetry(tt.err, tt.attempt, policy)
			if result != tt.expected {
				t.Errorf("shouldRetry(%v, attempt=%d) = %v, want %v",
					tt.err, tt.attempt, result, tt.expected)
			}
		})
	}
}

func TestRetrySuccessOnFirstAttempt(t *testing.T) {
	fs := memfs.New()
	rfs := New(fs).(*RetryFS)

	// Create a file successfully
	err := rfs.MkdirAll("/test", 0755)
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	// Check metrics
	metrics := rfs.GetMetrics()
	if metrics.TotalAttempts != 1 {
		t.Errorf("Expected 1 attempt, got %d", metrics.TotalAttempts)
	}
	if metrics.TotalRetries != 0 {
		t.Errorf("Expected 0 retries, got %d", metrics.TotalRetries)
	}
	if metrics.TotalSuccesses != 1 {
		t.Errorf("Expected 1 success, got %d", metrics.TotalSuccesses)
	}
}

func TestRetryWithTransientFailure(t *testing.T) {
	mock := &mockFailingFS{
		Filesystem:   memfs.New(),
		failuresLeft: 2, // Fail twice, then succeed
	}

	policy := &Policy{
		MaxAttempts: 5,
		BaseDelay:   1 * time.Millisecond, // Short delay for testing
		MaxDelay:    10 * time.Millisecond,
		Jitter:      0,
		Multiplier:  2.0,
	}

	rfs := New(mock, WithPolicy(policy)).(*RetryFS)

	// This should succeed after 2 retries
	err := rfs.MkdirAll("/test", 0755)
	if err != nil {
		t.Fatalf("Expected success after retries, got error: %v", err)
	}

	// Check metrics
	metrics := rfs.GetMetrics()
	if metrics.TotalAttempts != 3 { // Initial + 2 retries
		t.Errorf("Expected 3 attempts, got %d", metrics.TotalAttempts)
	}
	if metrics.TotalRetries != 2 {
		t.Errorf("Expected 2 retries, got %d", metrics.TotalRetries)
	}
	if metrics.TotalSuccesses != 1 {
		t.Errorf("Expected 1 success, got %d", metrics.TotalSuccesses)
	}
}

func TestRetryExhaustion(t *testing.T) {
	mock := &mockFailingFS{
		Filesystem:   memfs.New(),
		failuresLeft: 100, // Always fail
	}

	policy := &Policy{
		MaxAttempts: 3,
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    10 * time.Millisecond,
		Jitter:      0,
		Multiplier:  2.0,
	}

	rfs := New(mock, WithPolicy(policy)).(*RetryFS)

	// This should fail after exhausting retries
	err := rfs.MkdirAll("/test", 0755)
	if err == nil {
		t.Fatal("Expected error after exhausting retries")
	}

	// Check metrics
	metrics := rfs.GetMetrics()
	if metrics.TotalAttempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", metrics.TotalAttempts)
	}
	if metrics.TotalFailures != 1 {
		t.Errorf("Expected 1 failure, got %d", metrics.TotalFailures)
	}
}

func TestRetryPermanentError(t *testing.T) {
	fs := memfs.New()
	rfs := New(fs).(*RetryFS)

	// Try to stat a non-existent file (permanent error)
	_, err := rfs.Stat("/nonexistent")
	if err == nil {
		t.Fatal("Expected error for non-existent file")
	}

	// Should not retry permanent errors
	metrics := rfs.GetMetrics()
	if metrics.TotalAttempts != 1 {
		t.Errorf("Expected only 1 attempt for permanent error, got %d", metrics.TotalAttempts)
	}
	if metrics.TotalRetries != 0 {
		t.Errorf("Expected 0 retries for permanent error, got %d", metrics.TotalRetries)
	}
}

func TestOnRetryCallback(t *testing.T) {
	mock := &mockFailingFS{
		Filesystem:   memfs.New(),
		failuresLeft: 2,
	}

	retryCount := 0
	var lastOp Operation
	var lastErr error

	onRetry := func(op Operation, attempt int, err error) {
		retryCount++
		lastOp = op
		lastErr = err
	}

	policy := &Policy{
		MaxAttempts: 5,
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    10 * time.Millisecond,
		Jitter:      0,
		Multiplier:  2.0,
	}

	rfs := New(mock, WithPolicy(policy), WithOnRetry(onRetry))

	// This should trigger 2 retries
	_ = rfs.MkdirAll("/test", 0755)

	if retryCount != 2 {
		t.Errorf("Expected 2 retry callbacks, got %d", retryCount)
	}
	if lastOp != OpMkdirAll {
		t.Errorf("Expected operation MkdirAll, got %v", lastOp)
	}
	if lastErr == nil {
		t.Error("Expected error in callback")
	}
}

func TestPerOperationPolicies(t *testing.T) {
	config := &Config{
		DefaultPolicy: &Policy{
			MaxAttempts: 3,
			BaseDelay:   100 * time.Millisecond,
		},
		OperationPolicies: map[Operation]*Policy{
			OpStat: {
				MaxAttempts: 10, // More retries for stat
				BaseDelay:   50 * time.Millisecond,
			},
		},
		ErrorClassifier: defaultErrorClassifier,
	}

	// Verify policy selection
	statPolicy := config.GetPolicy(OpStat)
	if statPolicy.MaxAttempts != 10 {
		t.Errorf("Expected 10 max attempts for Stat, got %d", statPolicy.MaxAttempts)
	}

	mkdirPolicy := config.GetPolicy(OpMkdirAll)
	if mkdirPolicy.MaxAttempts != 3 {
		t.Errorf("Expected 3 max attempts for MkdirAll (default), got %d", mkdirPolicy.MaxAttempts)
	}
}

func TestCustomErrorClassifier(t *testing.T) {
	customErr := errors.New("custom retryable error")

	classifier := func(err error) ErrorClass {
		if err == customErr {
			return ErrorRetryable
		}
		return defaultErrorClassifier(err)
	}

	mock := &mockCustomErrorFS{
		Filesystem: memfs.New(),
		errorToReturn: customErr,
		failuresLeft:  2,
	}

	rfs := New(mock, WithErrorClassifier(classifier)).(*RetryFS)

	// Should retry custom error
	err := rfs.MkdirAll("/test", 0755)
	if err != nil {
		t.Fatalf("Expected success after retrying custom error, got: %v", err)
	}

	metrics := rfs.GetMetrics()
	if metrics.TotalRetries < 2 {
		t.Errorf("Expected at least 2 retries, got %d", metrics.TotalRetries)
	}
}

func TestBackoffMultiplier(t *testing.T) {
	tests := []struct {
		multiplier float64
		attempt    int
		expected   float64
	}{
		{2.0, 1, 100},   // 100 * 2^0
		{2.0, 2, 200},   // 100 * 2^1
		{2.0, 3, 400},   // 100 * 2^2
		{3.0, 1, 100},   // 100 * 3^0
		{3.0, 2, 300},   // 100 * 3^1
		{3.0, 3, 900},   // 100 * 3^2
		{1.5, 1, 100},   // 100 * 1.5^0
		{1.5, 2, 150},   // 100 * 1.5^1
		{1.5, 3, 225},   // 100 * 1.5^2
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("multiplier_%.1f_attempt_%d", tt.multiplier, tt.attempt), func(t *testing.T) {
			policy := &Policy{
				BaseDelay:  100 * time.Millisecond,
				MaxDelay:   10 * time.Second,
				Jitter:     0,
				Multiplier: tt.multiplier,
			}

			rfs := New(memfs.New()).(*RetryFS)
			delay := rfs.calculateBackoff(policy, tt.attempt)
			expected := time.Duration(tt.expected) * time.Millisecond

			// Allow small floating point errors
			diff := math.Abs(float64(delay - expected))
			if diff > 1000 { // 1 microsecond tolerance
				t.Errorf("calculateBackoff(multiplier=%.1f, attempt=%d) = %v, want %v",
					tt.multiplier, tt.attempt, delay, expected)
			}
		})
	}
}

// mockFailingFS simulates a filesystem that fails a certain number of times
type mockFailingFS struct {
	billy.Filesystem
	failuresLeft int
}

func (m *mockFailingFS) MkdirAll(path string, perm fs.FileMode) error {
	if m.failuresLeft > 0 {
		m.failuresLeft--
		return syscall.ETIMEDOUT // Retryable error
	}
	return m.Filesystem.MkdirAll(path, perm)
}

// mockCustomErrorFS returns a custom error
type mockCustomErrorFS struct {
	billy.Filesystem
	errorToReturn error
	failuresLeft  int
}

func (m *mockCustomErrorFS) MkdirAll(path string, perm fs.FileMode) error {
	if m.failuresLeft > 0 {
		m.failuresLeft--
		return m.errorToReturn
	}
	return m.Filesystem.MkdirAll(path, perm)
}
