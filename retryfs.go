package retryfs

import (
	"io/fs"
	"math"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/absfs/absfs"
)

// RetryFS wraps an absfs.FileSystem with automatic retry logic
type RetryFS struct {
	fs                        absfs.FileSystem
	config                    *Config
	metrics                   *Metrics
	circuitBreaker            *CircuitBreaker
	perOperationCircuitBreaker *PerOperationCircuitBreaker
	logger                    Logger
	mu                        sync.RWMutex
	rng                       *rand.Rand
}

// Option is a functional option for configuring RetryFS
type Option func(*RetryFS)

// New creates a new RetryFS wrapping the given filesystem
func New(fs absfs.FileSystem, options ...Option) absfs.FileSystem {
	rfs := &RetryFS{
		fs:      fs,
		config:  DefaultConfig(),
		metrics: NewMetrics(),
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	for _, opt := range options {
		opt(rfs)
	}

	return rfs
}

// WithPolicy sets the default retry policy
func WithPolicy(policy *Policy) Option {
	return func(rfs *RetryFS) {
		rfs.config.DefaultPolicy = policy
	}
}

// WithConfig sets the entire configuration
func WithConfig(config *Config) Option {
	return func(rfs *RetryFS) {
		rfs.config = config
	}
}

// WithErrorClassifier sets a custom error classifier
func WithErrorClassifier(classifier ErrorClassifier) Option {
	return func(rfs *RetryFS) {
		rfs.config.ErrorClassifier = classifier
	}
}

// WithOnRetry sets a callback for retry events
func WithOnRetry(onRetry func(op Operation, attempt int, err error)) Option {
	return func(rfs *RetryFS) {
		rfs.config.OnRetry = onRetry
	}
}

// WithCircuitBreaker enables circuit breaker protection
func WithCircuitBreaker(cb *CircuitBreaker) Option {
	return func(rfs *RetryFS) {
		rfs.circuitBreaker = cb
	}
}

// WithPerOperationCircuitBreaker enables per-operation circuit breaker protection
func WithPerOperationCircuitBreaker(pocb *PerOperationCircuitBreaker) Option {
	return func(rfs *RetryFS) {
		rfs.perOperationCircuitBreaker = pocb
	}
}

// GetMetrics returns the current metrics
func (rfs *RetryFS) GetMetrics() *Metrics {
	rfs.mu.RLock()
	defer rfs.mu.RUnlock()
	return rfs.metrics
}

// GetCircuitBreaker returns the circuit breaker if configured
func (rfs *RetryFS) GetCircuitBreaker() *CircuitBreaker {
	return rfs.circuitBreaker
}

// GetPerOperationCircuitBreaker returns the per-operation circuit breaker if configured
func (rfs *RetryFS) GetPerOperationCircuitBreaker() *PerOperationCircuitBreaker {
	return rfs.perOperationCircuitBreaker
}

// calculateBackoff calculates the backoff delay for a given attempt
// using exponential backoff with jitter
func (rfs *RetryFS) calculateBackoff(policy *Policy, attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}

	// Calculate base delay with exponential backoff
	// delay = baseDelay * multiplier^(attempt-1)
	multiplier := policy.Multiplier
	if multiplier <= 0 {
		multiplier = 2.0
	}

	delay := float64(policy.BaseDelay) * math.Pow(multiplier, float64(attempt-1))

	// Cap at max delay
	if maxDelay := float64(policy.MaxDelay); delay > maxDelay {
		delay = maxDelay
	}

	// Add jitter: delay * (1 ± jitter)
	if policy.Jitter > 0 {
		jitterRange := delay * policy.Jitter
		// Generate random value in range [-jitterRange, +jitterRange]
		rfs.mu.Lock()
		jitterDelta := (rfs.rng.Float64()*2 - 1) * jitterRange
		rfs.mu.Unlock()
		delay += jitterDelta
	}

	// Ensure delay is non-negative
	if delay < 0 {
		delay = 0
	}

	return time.Duration(delay)
}

// shouldRetry determines if an error should trigger a retry
func (rfs *RetryFS) shouldRetry(err error, attempt int, policy *Policy) bool {
	if err == nil {
		return false
	}

	// Check if we've exhausted retry attempts
	if attempt >= policy.MaxAttempts {
		return false
	}

	// Classify the error
	errClass := rfs.config.ErrorClassifier(err)

	// Don't retry permanent errors
	if errClass == ErrorPermanent {
		return false
	}

	// Retry retryable errors
	if errClass == ErrorRetryable {
		return true
	}

	// For unknown errors, retry conservatively
	// You could make this configurable
	return true
}

// retry executes a function with retry logic
func (rfs *RetryFS) retry(op Operation, fn func() error) error {
	// Wrap with per-operation circuit breaker if configured
	if rfs.perOperationCircuitBreaker != nil {
		return rfs.perOperationCircuitBreaker.Call(op, func() error {
			return rfs.retryWithoutCircuitBreaker(op, fn)
		})
	}

	// Wrap with global circuit breaker if configured
	if rfs.circuitBreaker != nil {
		return rfs.circuitBreaker.Call(func() error {
			return rfs.retryWithoutCircuitBreaker(op, fn)
		})
	}

	return rfs.retryWithoutCircuitBreaker(op, fn)
}

// retryWithoutCircuitBreaker executes a function with retry logic (without circuit breaker)
func (rfs *RetryFS) retryWithoutCircuitBreaker(op Operation, fn func() error) error {
	policy := rfs.config.GetPolicy(op)
	var lastErr error
	startTime := time.Now()

	for attempt := 0; attempt < policy.MaxAttempts; attempt++ {
		// Record the attempt
		isRetry := attempt > 0
		rfs.mu.Lock()
		rfs.metrics.RecordAttempt(op, isRetry)
		rfs.mu.Unlock()

		// Call the OnRetry callback if this is a retry
		if isRetry && rfs.config.OnRetry != nil {
			rfs.config.OnRetry(op, attempt, lastErr)
		}

		// Sleep before retry (not on first attempt)
		var backoff time.Duration
		if attempt > 0 {
			backoff = rfs.calculateBackoff(policy, attempt)
			// Log retry with structured logging
			rfs.logRetry(op, attempt, lastErr, backoff)
			time.Sleep(backoff)
		}

		// Execute the operation
		err := fn()

		// Success case
		if err == nil {
			rfs.mu.Lock()
			rfs.metrics.RecordSuccess(op)
			rfs.mu.Unlock()

			// Log successful operation
			duration := time.Since(startTime)
			rfs.logOperationComplete(op, attempt+1, nil, duration)
			return nil
		}

		// Store the error
		lastErr = err

		// Check if we should retry
		if !rfs.shouldRetry(err, attempt+1, policy) {
			break
		}
	}

	// All retries exhausted or permanent error
	rfs.mu.Lock()
	errClass := rfs.config.ErrorClassifier(lastErr)
	rfs.metrics.RecordFailure(op, errClass)
	rfs.mu.Unlock()

	// Log failed operation
	duration := time.Since(startTime)
	rfs.logOperationComplete(op, policy.MaxAttempts, lastErr, duration)

	return lastErr
}

// retryWithResult executes a function that returns a value and an error
func retryWithResult[T any](rfs *RetryFS, op Operation, fn func() (T, error)) (T, error) {
	var result T
	var resultErr error

	err := rfs.retry(op, func() error {
		var err error
		result, err = fn()
		resultErr = err
		return err
	})

	// If retry succeeded, return the result
	if err == nil {
		return result, nil
	}

	// Return the last result and error
	return result, resultErr
}

// Create implements absfs.FileSystem
func (rfs *RetryFS) Create(filename string) (absfs.File, error) {
	return retryWithResult(rfs, OpCreate, func() (absfs.File, error) {
		return rfs.fs.Create(filename)
	})
}

// Open implements absfs.FileSystem
func (rfs *RetryFS) Open(filename string) (absfs.File, error) {
	return retryWithResult(rfs, OpOpen, func() (absfs.File, error) {
		return rfs.fs.Open(filename)
	})
}

// OpenFile implements absfs.FileSystem
func (rfs *RetryFS) OpenFile(filename string, flag int, perm os.FileMode) (absfs.File, error) {
	return retryWithResult(rfs, OpOpenFile, func() (absfs.File, error) {
		return rfs.fs.OpenFile(filename, flag, perm)
	})
}

// Stat implements absfs.FileSystem
func (rfs *RetryFS) Stat(filename string) (os.FileInfo, error) {
	return retryWithResult(rfs, OpStat, func() (os.FileInfo, error) {
		return rfs.fs.Stat(filename)
	})
}

// Rename implements absfs.FileSystem
func (rfs *RetryFS) Rename(oldpath, newpath string) error {
	return rfs.retry(OpRename, func() error {
		return rfs.fs.Rename(oldpath, newpath)
	})
}

// Remove implements absfs.FileSystem
func (rfs *RetryFS) Remove(filename string) error {
	return rfs.retry(OpRemove, func() error {
		return rfs.fs.Remove(filename)
	})
}

// MkdirAll implements absfs.FileSystem
func (rfs *RetryFS) MkdirAll(filename string, perm os.FileMode) error {
	return rfs.retry(OpMkdirAll, func() error {
		return rfs.fs.MkdirAll(filename, perm)
	})
}

// Mkdir implements absfs.FileSystem
func (rfs *RetryFS) Mkdir(name string, perm os.FileMode) error {
	return rfs.retry(OpMkdir, func() error {
		return rfs.fs.Mkdir(name, perm)
	})
}

// RemoveAll implements absfs.FileSystem
func (rfs *RetryFS) RemoveAll(path string) error {
	return rfs.retry(OpRemoveAll, func() error {
		return rfs.fs.RemoveAll(path)
	})
}

// Chmod implements absfs.FileSystem
func (rfs *RetryFS) Chmod(name string, mode os.FileMode) error {
	return rfs.retry(OpChmod, func() error {
		return rfs.fs.Chmod(name, mode)
	})
}

// Chtimes implements absfs.FileSystem
func (rfs *RetryFS) Chtimes(name string, atime time.Time, mtime time.Time) error {
	return rfs.retry(OpChtimes, func() error {
		return rfs.fs.Chtimes(name, atime, mtime)
	})
}

// Chown implements absfs.FileSystem
func (rfs *RetryFS) Chown(name string, uid, gid int) error {
	return rfs.retry(OpChown, func() error {
		return rfs.fs.Chown(name, uid, gid)
	})
}

// Chdir implements absfs.FileSystem
func (rfs *RetryFS) Chdir(dir string) error {
	return rfs.retry(OpChdir, func() error {
		return rfs.fs.Chdir(dir)
	})
}

// Getwd implements absfs.FileSystem
func (rfs *RetryFS) Getwd() (string, error) {
	return retryWithResult(rfs, OpGetwd, func() (string, error) {
		return rfs.fs.Getwd()
	})
}

// TempDir implements absfs.FileSystem
func (rfs *RetryFS) TempDir() string {
	return rfs.fs.TempDir()
}

// Truncate implements absfs.FileSystem
func (rfs *RetryFS) Truncate(name string, size int64) error {
	return rfs.retry(OpTruncate, func() error {
		return rfs.fs.Truncate(name, size)
	})
}

// ReadDir implements absfs.FileSystem
func (rfs *RetryFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return retryWithResult(rfs, OpReadDir, func() ([]fs.DirEntry, error) {
		return rfs.fs.ReadDir(name)
	})
}

// ReadFile implements absfs.FileSystem
func (rfs *RetryFS) ReadFile(name string) ([]byte, error) {
	return retryWithResult(rfs, OpReadFile, func() ([]byte, error) {
		return rfs.fs.ReadFile(name)
	})
}

// Sub implements absfs.FileSystem
func (rfs *RetryFS) Sub(dir string) (fs.FS, error) {
	return retryWithResult(rfs, OpSub, func() (fs.FS, error) {
		return rfs.fs.Sub(dir)
	})
}

// Symlink operations - only available if the underlying filesystem supports them

// Lstat implements absfs.SymLinker
func (rfs *RetryFS) Lstat(name string) (os.FileInfo, error) {
	if sl, ok := rfs.fs.(absfs.SymLinker); ok {
		return retryWithResult(rfs, OpLstat, func() (os.FileInfo, error) {
			return sl.Lstat(name)
		})
	}
	// Fall back to Stat if symlinks not supported
	return rfs.Stat(name)
}

// Lchown implements absfs.SymLinker
func (rfs *RetryFS) Lchown(name string, uid, gid int) error {
	if sl, ok := rfs.fs.(absfs.SymLinker); ok {
		return rfs.retry(OpLchown, func() error {
			return sl.Lchown(name, uid, gid)
		})
	}
	return absfs.ErrNotImplemented
}

// Readlink implements absfs.SymLinker
func (rfs *RetryFS) Readlink(name string) (string, error) {
	if sl, ok := rfs.fs.(absfs.SymLinker); ok {
		return retryWithResult(rfs, OpReadlink, func() (string, error) {
			return sl.Readlink(name)
		})
	}
	return "", absfs.ErrNotImplemented
}

// Symlink implements absfs.SymLinker
func (rfs *RetryFS) Symlink(oldname, newname string) error {
	if sl, ok := rfs.fs.(absfs.SymLinker); ok {
		return rfs.retry(OpSymlink, func() error {
			return sl.Symlink(oldname, newname)
		})
	}
	return absfs.ErrNotImplemented
}
