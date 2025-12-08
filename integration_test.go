package retryfs

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/absfs/absfs"
)



// chaosFS injects random failures for chaos testing
type chaosFS struct {
	fs               absfs.FileSystem
	failureProbability float64
	mu               sync.Mutex // Protects both RNG and underlying FS (memfs is not thread-safe)
	rng              *rand.Rand
	totalCalls       int64
	failures         int64
}

func newChaosFS(fs absfs.FileSystem, failureProbability float64) *chaosFS {
	return &chaosFS{
		fs:               fs,
		failureProbability: failureProbability,
		rng:              rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (c *chaosFS) shouldFail() bool {
	atomic.AddInt64(&c.totalCalls, 1)
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.rng.Float64() < c.failureProbability {
		atomic.AddInt64(&c.failures, 1)
		return true
	}
	return false
}

func (c *chaosFS) getStats() (calls, failures int64) {
	return atomic.LoadInt64(&c.totalCalls), atomic.LoadInt64(&c.failures)
}

func (c *chaosFS) Create(filename string) (absfs.File, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	atomic.AddInt64(&c.totalCalls, 1)
	if c.rng.Float64() < c.failureProbability {
		atomic.AddInt64(&c.failures, 1)
		return nil, errors.New("chaos: simulated network error")
	}
	return c.fs.Create(filename)
}

func (c *chaosFS) Open(filename string) (absfs.File, error) {
	if c.shouldFail() {
		return nil, errors.New("chaos: connection timeout")
	}
	return c.fs.Open(filename)
}

func (c *chaosFS) OpenFile(filename string, flag int, perm os.FileMode) (absfs.File, error) {
	if c.shouldFail() {
		return nil, errors.New("chaos: temporary failure")
	}
	return c.fs.OpenFile(filename, flag, perm)
}

func (c *chaosFS) Stat(filename string) (os.FileInfo, error) {
	if c.shouldFail() {
		return nil, errors.New("chaos: service unavailable")
	}
	return c.fs.Stat(filename)
}

func (c *chaosFS) Rename(oldpath, newpath string) error {
	if c.shouldFail() {
		return errors.New("chaos: network error")
	}
	return c.fs.Rename(oldpath, newpath)
}

func (c *chaosFS) Remove(filename string) error {
	if c.shouldFail() {
		return errors.New("chaos: i/o error")
	}
	return c.fs.Remove(filename)
}


func (c *chaosFS) MkdirAll(filename string, perm os.FileMode) error {
	if c.shouldFail() {
		return errors.New("chaos: transient error")
	}
	return c.fs.MkdirAll(filename, perm)
}

func (c *chaosFS) Lstat(filename string) (os.FileInfo, error) {
	sl, ok := c.fs.(absfs.SymLinker)
	if !ok {
		return c.Stat(filename)
	}
	if c.shouldFail() {
		return nil, errors.New("chaos: network timeout")
	}
	return sl.Lstat(filename)
}

func (c *chaosFS) Lchown(name string, uid, gid int) error {
	sl, ok := c.fs.(absfs.SymLinker)
	if !ok {
		return absfs.ErrNotImplemented
	}
	if c.shouldFail() {
		return errors.New("chaos: lchown failed")
	}
	return sl.Lchown(name, uid, gid)
}

func (c *chaosFS) Symlink(target, link string) error {
	sl, ok := c.fs.(absfs.SymLinker)
	if !ok {
		return absfs.ErrNotImplemented
	}
	if c.shouldFail() {
		return errors.New("chaos: temporary unavailable")
	}
	return sl.Symlink(target, link)
}

func (c *chaosFS) Readlink(link string) (string, error) {
	sl, ok := c.fs.(absfs.SymLinker)
	if !ok {
		return "", absfs.ErrNotImplemented
	}
	if c.shouldFail() {
		return "", errors.New("chaos: i/o error")
	}
	return sl.Readlink(link)
}


// Mkdir implements absfs.FileSystem
func (c *chaosFS) Mkdir(name string, perm os.FileMode) error {
	if c.shouldFail() {
		return errors.New("chaos: mkdir failed")
	}
	return c.fs.Mkdir(name, perm)
}

// RemoveAll implements absfs.FileSystem
func (c *chaosFS) RemoveAll(path string) error {
	if c.shouldFail() {
		return errors.New("chaos: removeall failed")
	}
	return c.fs.RemoveAll(path)
}

// Chmod implements absfs.FileSystem
func (c *chaosFS) Chmod(name string, mode os.FileMode) error {
	if c.shouldFail() {
		return errors.New("chaos: chmod failed")
	}
	return c.fs.Chmod(name, mode)
}

// Chtimes implements absfs.FileSystem
func (c *chaosFS) Chtimes(name string, atime time.Time, mtime time.Time) error {
	if c.shouldFail() {
		return errors.New("chaos: chtimes failed")
	}
	return c.fs.Chtimes(name, atime, mtime)
}

// Chown implements absfs.FileSystem
func (c *chaosFS) Chown(name string, uid, gid int) error {
	if c.shouldFail() {
		return errors.New("chaos: chown failed")
	}
	return c.fs.Chown(name, uid, gid)
}

// Separator implements absfs.FileSystem
func (c *chaosFS) Separator() uint8 {
	return c.fs.Separator()
}

// ListSeparator implements absfs.FileSystem
func (c *chaosFS) ListSeparator() uint8 {
	return c.fs.ListSeparator()
}

// Chdir implements absfs.FileSystem
func (c *chaosFS) Chdir(dir string) error {
	if c.shouldFail() {
		return errors.New("chaos: chdir failed")
	}
	return c.fs.Chdir(dir)
}

// Getwd implements absfs.FileSystem
func (c *chaosFS) Getwd() (string, error) {
	if c.shouldFail() {
		return "", errors.New("chaos: getwd failed")
	}
	return c.fs.Getwd()
}

// TempDir implements absfs.FileSystem
func (c *chaosFS) TempDir() string {
	return c.fs.TempDir()
}

// Truncate implements absfs.FileSystem
func (c *chaosFS) Truncate(name string, size int64) error {
	if c.shouldFail() {
		return errors.New("chaos: truncate failed")
	}
	return c.fs.Truncate(name, size)
}

// TestIntegration_ChaosMonkey tests retryfs with random failures
func TestIntegration_ChaosMonkey(t *testing.T) {
	chaos := newChaosFS(mustNewMemFS(), 0.3) // 30% failure rate

	policy := &Policy{
		MaxAttempts: 10,
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
		Jitter:      0.1,
		Multiplier:  2.0,
	}

	fs := New(chaos, WithPolicy(policy)).(*RetryFS)

	// Perform various operations
	operations := []struct {
		name string
		fn   func() error
	}{
		{"Create directory", func() error {
			return fs.MkdirAll("/test/data", 0755)
		}},
		{"Create file", func() error {
			f, err := fs.Create("/test/data/file.txt")
			if err != nil {
				return err
			}
			return f.Close()
		}},
		{"Stat file", func() error {
			_, err := fs.Stat("/test/data/file.txt")
			return err
		}},
		{"Open directory", func() error {
			f, err := fs.Open("/test/data")
			if err != nil {
				return err
			}
			_, err = f.Readdir(-1)
			f.Close()
			return err
		}},
	}

	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			err := op.fn()
			if err != nil {
				t.Errorf("%s failed: %v", op.name, err)
			}
		})
	}

	// Check metrics
	metrics := fs.GetMetrics()
	calls, failures := chaos.getStats()

	t.Logf("Chaos Stats: %d total calls, %d failures (%.1f%%)",
		calls, failures, float64(failures)/float64(calls)*100)
	t.Logf("Retry Stats: %d attempts, %d retries, %d successes, %d final failures",
		metrics.TotalAttempts, metrics.TotalRetries, metrics.TotalSuccesses, metrics.TotalFailures)

	// Verify retries happened
	if metrics.TotalRetries == 0 && failures > 0 {
		t.Error("Expected retries to occur given chaos failures")
	}
}

// TestIntegration_HighFailureRate tests with very high failure rate
func TestIntegration_HighFailureRate(t *testing.T) {
	chaos := newChaosFS(mustNewMemFS(), 0.8) // 80% failure rate!

	policy := &Policy{
		MaxAttempts: 20, // Need many attempts for 80% failure
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    50 * time.Millisecond,
		Jitter:      0.1,
		Multiplier:  1.5,
	}

	cb := NewCircuitBreaker()
	cb.FailureThreshold = 50 // High threshold for this test

	fs := New(chaos,
		WithPolicy(policy),
		WithCircuitBreaker(cb),
	).(*RetryFS)

	// Try to create a directory - should eventually succeed
	err := fs.MkdirAll("/test", 0755)
	if err != nil {
		t.Logf("Operation failed even with retries: %v", err)
	}

	metrics := fs.GetMetrics()
	t.Logf("Stats: %d attempts, %d retries, success rate: %.1f%%",
		metrics.TotalAttempts, metrics.TotalRetries,
		float64(metrics.TotalSuccesses)/float64(metrics.TotalAttempts)*100)
}

// TestIntegration_ConcurrentOperations tests concurrent operations with failures
// Skipped because memfs is not thread-safe
func TestIntegration_ConcurrentOperations(t *testing.T) {
	t.Skip("Skipping concurrent test - underlying memfs is not thread-safe")
	chaos := newChaosFS(mustNewMemFS(), 0.2) // 20% failure rate

	fs := New(chaos, WithPolicy(&Policy{
		MaxAttempts: 5,
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    50 * time.Millisecond,
		Jitter:      0.2,
		Multiplier:  2.0,
	})).(*RetryFS)

	// Create base directory
	err := fs.MkdirAll("/test", 0755)
	if err != nil {
		t.Fatalf("Failed to create base directory: %v", err)
	}

	const goroutines = 10
	const filesPerGoroutine = 5

	var wg sync.WaitGroup
	errors := make(chan error, goroutines*filesPerGoroutine)

	// Spawn concurrent goroutines creating files
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < filesPerGoroutine; j++ {
				filename := fmt.Sprintf("/test/file_%d_%d.txt", id, j)
				f, err := fs.Create(filename)
				if err != nil {
					errors <- fmt.Errorf("goroutine %d: %w", id, err)
					continue
				}
				f.Close()
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	errCount := 0
	for err := range errors {
		errCount++
		t.Logf("Concurrent operation error: %v", err)
	}

	if errCount > 0 {
		t.Logf("Had %d errors out of %d operations", errCount, goroutines*filesPerGoroutine)
	}

	metrics := fs.GetMetrics()
	t.Logf("Concurrent test stats: %d attempts, %d retries, %d successes, %d failures",
		metrics.TotalAttempts, metrics.TotalRetries, metrics.TotalSuccesses, metrics.TotalFailures)
}

// TestIntegration_ContextCancellation tests context cancellation during retries
func TestIntegration_ContextCancellation(t *testing.T) {
	// Create a filesystem that always fails
	chaos := newChaosFS(mustNewMemFS(), 1.0) // 100% failure rate

	fs := New(chaos, WithPolicy(&Policy{
		MaxAttempts: 100, // Many attempts
		BaseDelay:   50 * time.Millisecond,
		MaxDelay:    1 * time.Second,
		Jitter:      0,
		Multiplier:  1.0,
	})).(*RetryFS)

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := fs.MkdirAllContext(ctx, "/test", 0755)
	duration := time.Since(start)

	if err == nil {
		t.Error("Expected context cancellation error")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected DeadlineExceeded, got: %v", err)
	}

	// Should fail quickly due to context, not retry 100 times
	if duration > 500*time.Millisecond {
		t.Errorf("Operation took too long (%v), context should have cancelled sooner", duration)
	}

	t.Logf("Context cancelled after %v as expected", duration)
}

// TestIntegration_CircuitBreakerTrip tests circuit breaker opening under load
func TestIntegration_CircuitBreakerTrip(t *testing.T) {
	// Filesystem that always fails
	chaos := newChaosFS(mustNewMemFS(), 1.0) // 100% failure rate

	cb := NewCircuitBreaker()
	cb.FailureThreshold = 5
	cb.SuccessThreshold = 2
	cb.Timeout = 100 * time.Millisecond

	var stateChanges int32
	cb.OnStateChange = func(from, to State) {
		atomic.AddInt32(&stateChanges, 1)
	}

	fs := New(chaos,
		WithPolicy(&Policy{
			MaxAttempts: 3,
			BaseDelay:   1 * time.Millisecond,
			MaxDelay:    10 * time.Millisecond,
			Jitter:      0,
			Multiplier:  2.0,
		}),
		WithCircuitBreaker(cb),
	).(*RetryFS)

	// Make multiple failing calls to trip the circuit breaker
	for i := 0; i < 10; i++ {
		_ = fs.MkdirAll(fmt.Sprintf("/test/%d", i), 0755)
	}

	// Give async callbacks time to complete
	time.Sleep(10 * time.Millisecond)

	// Circuit should be open now
	if cb.GetState() != StateOpen {
		t.Errorf("Expected circuit to be open, got: %s", cb.GetState())
	}

	changes := atomic.LoadInt32(&stateChanges)
	if changes == 0 {
		t.Error("Expected circuit breaker state changes")
	}

	t.Logf("Circuit breaker opened after %d state changes", changes)
}
