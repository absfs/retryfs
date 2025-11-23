package retryfs

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
)

func TestContextCancellation(t *testing.T) {
	mock := &mockFailingFS{
		Filesystem:   memfs.New(),
		failuresLeft: 100, // Always fail
	}

	policy := &Policy{
		MaxAttempts: 10,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    1 * time.Second,
		Jitter:      0,
		Multiplier:  2.0,
	}

	rfs := New(mock, WithPolicy(policy)).(*RetryFS)

	// Create a context that will be canceled
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := rfs.MkdirAllContext(ctx, "/test", 0755)
	duration := time.Since(start)

	// Should fail with context canceled error
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled error, got %v", err)
	}

	// Should have returned quickly (not waited for all retries)
	if duration > 200*time.Millisecond {
		t.Errorf("Expected quick return on cancel, took %v", duration)
	}
}

func TestContextDeadline(t *testing.T) {
	mock := &mockFailingFS{
		Filesystem:   memfs.New(),
		failuresLeft: 100, // Always fail
	}

	policy := &Policy{
		MaxAttempts: 10,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    1 * time.Second,
		Jitter:      0,
		Multiplier:  2.0,
	}

	rfs := New(mock, WithPolicy(policy)).(*RetryFS)

	// Create a context with a deadline
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := rfs.MkdirAllContext(ctx, "/test", 0755)
	duration := time.Since(start)

	// Should fail with deadline exceeded error
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected context.DeadlineExceeded error, got %v", err)
	}

	// Should have returned around the deadline
	if duration > 250*time.Millisecond {
		t.Errorf("Expected return near deadline, took %v", duration)
	}
}

func TestContextSuccess(t *testing.T) {
	fs := memfs.New()
	rfs := New(fs).(*RetryFS)

	ctx := context.Background()

	// Test successful operations with context
	err := rfs.MkdirAllContext(ctx, "/test", 0755)
	if err != nil {
		t.Errorf("Expected success, got %v", err)
	}

	file, err := rfs.CreateContext(ctx, "/test/file.txt")
	if err != nil {
		t.Errorf("Expected success, got %v", err)
	}
	file.Close()

	info, err := rfs.StatContext(ctx, "/test/file.txt")
	if err != nil {
		t.Errorf("Expected success, got %v", err)
	}
	if info.Name() != "file.txt" {
		t.Errorf("Expected file.txt, got %s", info.Name())
	}
}

func TestContextCanceledDuringBackoff(t *testing.T) {
	mock := &mockFailingFS{
		Filesystem:   memfs.New(),
		failuresLeft: 100, // Always fail
	}

	policy := &Policy{
		MaxAttempts: 5,
		BaseDelay:   200 * time.Millisecond,
		MaxDelay:    1 * time.Second,
		Jitter:      0,
		Multiplier:  2.0,
	}

	rfs := New(mock, WithPolicy(policy)).(*RetryFS)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel during the first retry backoff
	go func() {
		time.Sleep(250 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := rfs.MkdirAllContext(ctx, "/test", 0755)
	duration := time.Since(start)

	// Should fail with context canceled error
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled error, got %v", err)
	}

	// Should return during backoff, not after
	if duration < 200*time.Millisecond || duration > 400*time.Millisecond {
		t.Errorf("Expected return during first backoff (~250ms), took %v", duration)
	}
}

func TestContextWithTransientFailures(t *testing.T) {
	mock := &mockFailingFS{
		Filesystem:   memfs.New(),
		failuresLeft: 2, // Fail twice then succeed
	}

	policy := &Policy{
		MaxAttempts: 5,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
		Jitter:      0,
		Multiplier:  2.0,
	}

	rfs := New(mock, WithPolicy(policy)).(*RetryFS)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Should succeed after retries
	err := rfs.MkdirAllContext(ctx, "/test", 0755)
	if err != nil {
		t.Errorf("Expected success after retries, got %v", err)
	}
}

func TestAllContextMethods(t *testing.T) {
	fs := memfs.New()
	rfs := New(fs).(*RetryFS)

	ctx := context.Background()

	// Create directory
	if err := rfs.MkdirAllContext(ctx, "/test", 0755); err != nil {
		t.Fatalf("MkdirAllContext failed: %v", err)
	}

	// Create file
	file, err := rfs.CreateContext(ctx, "/test/file.txt")
	if err != nil {
		t.Fatalf("CreateContext failed: %v", err)
	}
	file.Close()

	// Open file
	file, err = rfs.OpenContext(ctx, "/test/file.txt")
	if err != nil {
		t.Fatalf("OpenContext failed: %v", err)
	}
	file.Close()

	// OpenFile
	file, err = rfs.OpenFileContext(ctx, "/test/file.txt", 0, 0644)
	if err != nil {
		t.Fatalf("OpenFileContext failed: %v", err)
	}
	file.Close()

	// Stat
	_, err = rfs.StatContext(ctx, "/test/file.txt")
	if err != nil {
		t.Fatalf("StatContext failed: %v", err)
	}

	// Lstat
	_, err = rfs.LstatContext(ctx, "/test/file.txt")
	if err != nil {
		t.Fatalf("LstatContext failed: %v", err)
	}

	// ReadDir
	_, err = rfs.ReadDirContext(ctx, "/test")
	if err != nil {
		t.Fatalf("ReadDirContext failed: %v", err)
	}

	// Rename
	if err := rfs.RenameContext(ctx, "/test/file.txt", "/test/renamed.txt"); err != nil {
		t.Fatalf("RenameContext failed: %v", err)
	}

	// Remove
	if err := rfs.RemoveContext(ctx, "/test/renamed.txt"); err != nil {
		t.Fatalf("RemoveContext failed: %v", err)
	}

	// TempFile
	tmpFile, err := rfs.TempFileContext(ctx, "/test", "tmp")
	if err != nil {
		t.Fatalf("TempFileContext failed: %v", err)
	}
	tmpFile.Close()
}

func TestContextWithCircuitBreaker(t *testing.T) {
	mock := &mockFailingFS{
		Filesystem:   memfs.New(),
		failuresLeft: 100, // Always fail
	}

	cb := &CircuitBreaker{
		FailureThreshold: 2,
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

	ctx := context.Background()

	// First two calls should fail and open the circuit
	rfs.MkdirAllContext(ctx, "/test1", 0755)
	rfs.MkdirAllContext(ctx, "/test2", 0755)

	if cb.GetState() != StateOpen {
		t.Errorf("Expected circuit to be open, got %v", cb.GetState())
	}

	// Next call should fail fast with ErrCircuitOpen
	err := rfs.MkdirAllContext(ctx, "/test3", 0755)
	if err != ErrCircuitOpen {
		t.Errorf("Expected ErrCircuitOpen, got %v", err)
	}
}

func TestContextCanceledBeforeFirstAttempt(t *testing.T) {
	fs := memfs.New()
	rfs := New(fs).(*RetryFS)

	// Create already-canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := rfs.MkdirAllContext(ctx, "/test", 0755)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled error, got %v", err)
	}
}

func TestContextWithRetryableErrors(t *testing.T) {
	mock := &mockCustomErrorFS{
		Filesystem:    memfs.New(),
		errorToReturn: syscall.ETIMEDOUT,
		failuresLeft:  3,
	}

	policy := &Policy{
		MaxAttempts: 5,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
		Jitter:      0,
		Multiplier:  2.0,
	}

	rfs := New(mock, WithPolicy(policy)).(*RetryFS)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Should succeed after 3 retries
	err := rfs.MkdirAllContext(ctx, "/test", 0755)
	if err != nil {
		t.Errorf("Expected success after retries, got %v", err)
	}
}

func TestContextPropagationToChmod(t *testing.T) {
	fs := memfs.New()
	rfs := New(fs).(*RetryFS)

	ctx := context.Background()

	// Create a file first
	err := rfs.MkdirAllContext(ctx, "/test", 0755)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	file, err := rfs.CreateContext(ctx, "/test/file.txt")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	file.Close()

	// Test Chmod with context (may not be supported by all filesystems)
	err = rfs.ChmodContext(ctx, "/test/file.txt", 0644)
	if err != nil {
		// memfs doesn't support Chmod, so we expect an error
		// Just verify the context was passed through
		if errors.Is(err, context.Canceled) {
			t.Error("Should not get context cancellation on unsupported operation")
		}
		// Skip test if operation not supported
		t.Skipf("Chmod not supported by filesystem: %v", err)
	}
}

func TestContextMetricsRecording(t *testing.T) {
	mock := &mockFailingFS{
		Filesystem:   memfs.New(),
		failuresLeft: 2,
	}

	policy := &Policy{
		MaxAttempts: 5,
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    10 * time.Millisecond,
		Jitter:      0,
		Multiplier:  2.0,
	}

	rfs := New(mock, WithPolicy(policy)).(*RetryFS)

	ctx := context.Background()

	// This should succeed after 2 retries
	err := rfs.MkdirAllContext(ctx, "/test", 0755)
	if err != nil {
		t.Fatalf("Expected success, got %v", err)
	}

	metrics := rfs.GetMetrics()
	if metrics.TotalAttempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", metrics.TotalAttempts)
	}
	if metrics.TotalRetries != 2 {
		t.Errorf("Expected 2 retries, got %d", metrics.TotalRetries)
	}
	if metrics.TotalSuccesses != 1 {
		t.Errorf("Expected 1 success, got %d", metrics.TotalSuccesses)
	}
}
