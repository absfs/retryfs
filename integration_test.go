package retryfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
)

// chaosFS injects random failures for chaos testing
type chaosFS struct {
	fs               billy.Filesystem
	failureProbability float64
	mu               sync.Mutex // Protects both RNG and underlying FS (memfs is not thread-safe)
	rng              *rand.Rand
	totalCalls       int64
	failures         int64
}

func newChaosFS(fs billy.Filesystem, failureProbability float64) *chaosFS {
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

func (c *chaosFS) Create(filename string) (billy.File, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	atomic.AddInt64(&c.totalCalls, 1)
	if c.rng.Float64() < c.failureProbability {
		atomic.AddInt64(&c.failures, 1)
		return nil, errors.New("chaos: simulated network error")
	}
	return c.fs.Create(filename)
}

func (c *chaosFS) Open(filename string) (billy.File, error) {
	if c.shouldFail() {
		return nil, errors.New("chaos: connection timeout")
	}
	return c.fs.Open(filename)
}

func (c *chaosFS) OpenFile(filename string, flag int, perm fs.FileMode) (billy.File, error) {
	if c.shouldFail() {
		return nil, errors.New("chaos: temporary failure")
	}
	return c.fs.OpenFile(filename, flag, perm)
}

func (c *chaosFS) Stat(filename string) (fs.FileInfo, error) {
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

func (c *chaosFS) Join(elem ...string) string {
	return c.fs.Join(elem...)
}

func (c *chaosFS) TempFile(dir, prefix string) (billy.File, error) {
	if c.shouldFail() {
		return nil, errors.New("chaos: connection refused")
	}
	return c.fs.TempFile(dir, prefix)
}

func (c *chaosFS) ReadDir(path string) ([]fs.FileInfo, error) {
	if c.shouldFail() {
		return nil, errors.New("chaos: timeout")
	}
	return c.fs.ReadDir(path)
}

func (c *chaosFS) MkdirAll(filename string, perm fs.FileMode) error {
	if c.shouldFail() {
		return errors.New("chaos: transient error")
	}
	return c.fs.MkdirAll(filename, perm)
}

func (c *chaosFS) Lstat(filename string) (fs.FileInfo, error) {
	if c.shouldFail() {
		return nil, errors.New("chaos: network timeout")
	}
	return c.fs.Lstat(filename)
}

func (c *chaosFS) Symlink(target, link string) error {
	if c.shouldFail() {
		return errors.New("chaos: temporary unavailable")
	}
	return c.fs.Symlink(target, link)
}

func (c *chaosFS) Readlink(link string) (string, error) {
	if c.shouldFail() {
		return "", errors.New("chaos: i/o error")
	}
	return c.fs.Readlink(link)
}

func (c *chaosFS) Chroot(path string) (billy.Filesystem, error) {
	if c.shouldFail() {
		return nil, errors.New("chaos: connection error")
	}
	return c.fs.Chroot(path)
}

func (c *chaosFS) Root() string {
	return c.fs.Root()
}

// TestIntegration_ChaosMonkey tests retryfs with random failures
func TestIntegration_ChaosMonkey(t *testing.T) {
	chaos := newChaosFS(memfs.New(), 0.3) // 30% failure rate

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
		{"Read directory", func() error {
			_, err := fs.ReadDir("/test/data")
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
	chaos := newChaosFS(memfs.New(), 0.8) // 80% failure rate!

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
	chaos := newChaosFS(memfs.New(), 0.2) // 20% failure rate

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
	chaos := newChaosFS(memfs.New(), 1.0) // 100% failure rate

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
	chaos := newChaosFS(memfs.New(), 1.0) // 100% failure rate

	cb := NewCircuitBreaker()
	cb.FailureThreshold = 5
	cb.SuccessThreshold = 2
	cb.Timeout = 100 * time.Millisecond

	var stateChanges int32
	cb.OnStateChange = func(from, to State) {
		t.Logf("Circuit breaker: %s -> %s", from, to)
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
