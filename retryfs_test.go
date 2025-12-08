package retryfs

import (
	"errors"
	"fmt"
	"math"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/absfs/absfs"
)


// failingFS wraps a filesystem to simulate failures for testing
type failingFS struct {
	fs           absfs.FileSystem
	failuresLeft int
	failError    error
}

func newFailingFS(fs absfs.FileSystem, failures int) *failingFS {
	return &failingFS{
		fs:           fs,
		failuresLeft: failures,
		failError:    errors.New("simulated transient failure"),
	}
}

func (f *failingFS) shouldFail() bool {
	if f.failuresLeft > 0 {
		f.failuresLeft--
		return true
	}
	return false
}

func (f *failingFS) Create(filename string) (absfs.File, error) {
	if f.shouldFail() {
		return nil, f.failError
	}
	return f.fs.Create(filename)
}

func (f *failingFS) Open(filename string) (absfs.File, error) {
	if f.shouldFail() {
		return nil, f.failError
	}
	return f.fs.Open(filename)
}

func (f *failingFS) OpenFile(filename string, flag int, perm os.FileMode) (absfs.File, error) {
	if f.shouldFail() {
		return nil, f.failError
	}
	return f.fs.OpenFile(filename, flag, perm)
}

func (f *failingFS) Stat(filename string) (os.FileInfo, error) {
	if f.shouldFail() {
		return nil, f.failError
	}
	return f.fs.Stat(filename)
}

func (f *failingFS) Rename(oldpath, newpath string) error {
	if f.shouldFail() {
		return f.failError
	}
	return f.fs.Rename(oldpath, newpath)
}

func (f *failingFS) Remove(filename string) error {
	if f.shouldFail() {
		return f.failError
	}
	return f.fs.Remove(filename)
}

func (f *failingFS) Mkdir(name string, perm os.FileMode) error {
	if f.shouldFail() {
		return f.failError
	}
	return f.fs.Mkdir(name, perm)
}

func (f *failingFS) MkdirAll(filename string, perm os.FileMode) error {
	if f.shouldFail() {
		return f.failError
	}
	return f.fs.MkdirAll(filename, perm)
}

func (f *failingFS) RemoveAll(path string) error {
	if f.shouldFail() {
		return f.failError
	}
	return f.fs.RemoveAll(path)
}

func (f *failingFS) Chmod(name string, mode os.FileMode) error {
	if f.shouldFail() {
		return f.failError
	}
	return f.fs.Chmod(name, mode)
}

func (f *failingFS) Chtimes(name string, atime time.Time, mtime time.Time) error {
	if f.shouldFail() {
		return f.failError
	}
	return f.fs.Chtimes(name, atime, mtime)
}

func (f *failingFS) Chown(name string, uid, gid int) error {
	if f.shouldFail() {
		return f.failError
	}
	return f.fs.Chown(name, uid, gid)
}

func (f *failingFS) Separator() uint8 {
	return f.fs.Separator()
}

func (f *failingFS) ListSeparator() uint8 {
	return f.fs.ListSeparator()
}

func (f *failingFS) Chdir(dir string) error {
	if f.shouldFail() {
		return f.failError
	}
	return f.fs.Chdir(dir)
}

func (f *failingFS) Getwd() (string, error) {
	if f.shouldFail() {
		return "", f.failError
	}
	return f.fs.Getwd()
}

func (f *failingFS) TempDir() string {
	return f.fs.TempDir()
}

func (f *failingFS) Truncate(name string, size int64) error {
	if f.shouldFail() {
		return f.failError
	}
	return f.fs.Truncate(name, size)
}

// Symlink operations
func (f *failingFS) Lstat(filename string) (os.FileInfo, error) {
	if sl, ok := f.fs.(absfs.SymLinker); ok {
		if f.shouldFail() {
			return nil, f.failError
		}
		return sl.Lstat(filename)
	}
	return f.Stat(filename)
}

func (f *failingFS) Lchown(name string, uid, gid int) error {
	if sl, ok := f.fs.(absfs.SymLinker); ok {
		if f.shouldFail() {
			return f.failError
		}
		return sl.Lchown(name, uid, gid)
	}
	return absfs.ErrNotImplemented
}

func (f *failingFS) Symlink(oldname, newname string) error {
	if sl, ok := f.fs.(absfs.SymLinker); ok {
		if f.shouldFail() {
			return f.failError
		}
		return sl.Symlink(oldname, newname)
	}
	return absfs.ErrNotImplemented
}

func (f *failingFS) Readlink(name string) (string, error) {
	if sl, ok := f.fs.(absfs.SymLinker); ok {
		if f.shouldFail() {
			return "", f.failError
		}
		return sl.Readlink(name)
	}
	return "", absfs.ErrNotImplemented
}

func TestCalculateBackoff(t *testing.T) {
	policy := &Policy{
		BaseDelay:  100 * time.Millisecond,
		MaxDelay:   5 * time.Second,
		Jitter:     0.0, // No jitter for predictable testing
		Multiplier: 2.0,
	}

	mfs := mustNewMemFS()
	rfs := New(mfs).(*RetryFS)

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

	rfs := New(mustNewMemFS()).(*RetryFS)

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

	rfs := New(mustNewMemFS(), WithPolicy(policy)).(*RetryFS)

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
		{"permanent error", os.ErrNotExist, 0, false},
		{"permanent error, attempt 1", os.ErrNotExist, 1, false},
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
	fs := mustNewMemFS()
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
		FileSystem:   mustNewMemFS(),
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
		FileSystem:   mustNewMemFS(),
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
	fs := mustNewMemFS()
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
		FileSystem:   mustNewMemFS(),
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
		FileSystem: mustNewMemFS(),
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

			rfs := New(mustNewMemFS()).(*RetryFS)
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
	absfs.FileSystem
	failuresLeft int
}

func (m *mockFailingFS) MkdirAll(path string, perm os.FileMode) error {
	if m.failuresLeft > 0 {
		m.failuresLeft--
		return syscall.ETIMEDOUT // Retryable error
	}
	return m.FileSystem.MkdirAll(path, perm)
}

// mockCustomErrorFS returns a custom error
type mockCustomErrorFS struct {
	absfs.FileSystem
	errorToReturn error
	failuresLeft  int
}

func (m *mockCustomErrorFS) MkdirAll(path string, perm os.FileMode) error {
	if m.failuresLeft > 0 {
		m.failuresLeft--
		return m.errorToReturn
	}
	return m.FileSystem.MkdirAll(path, perm)
}
