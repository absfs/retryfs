package retryfs

import (
	"io/fs"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/go-git/go-billy/v5"
)

// RetryFS wraps a billy.Filesystem with automatic retry logic
type RetryFS struct {
	fs      billy.Filesystem
	config  *Config
	metrics *Metrics
	mu      sync.RWMutex
	rng     *rand.Rand
}

// Option is a functional option for configuring RetryFS
type Option func(*RetryFS)

// New creates a new RetryFS wrapping the given filesystem
func New(fs billy.Filesystem, options ...Option) billy.Filesystem {
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

// GetMetrics returns the current metrics
func (rfs *RetryFS) GetMetrics() *Metrics {
	rfs.mu.RLock()
	defer rfs.mu.RUnlock()
	return rfs.metrics
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
	policy := rfs.config.GetPolicy(op)
	var lastErr error

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
		if attempt > 0 {
			backoff := rfs.calculateBackoff(policy, attempt)
			time.Sleep(backoff)
		}

		// Execute the operation
		err := fn()

		// Success case
		if err == nil {
			rfs.mu.Lock()
			rfs.metrics.RecordSuccess(op)
			rfs.mu.Unlock()
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

// Create implements billy.Filesystem
func (rfs *RetryFS) Create(filename string) (billy.File, error) {
	return retryWithResult(rfs, OpCreate, func() (billy.File, error) {
		return rfs.fs.Create(filename)
	})
}

// Open implements billy.Filesystem
func (rfs *RetryFS) Open(filename string) (billy.File, error) {
	return retryWithResult(rfs, OpOpen, func() (billy.File, error) {
		return rfs.fs.Open(filename)
	})
}

// OpenFile implements billy.Filesystem
func (rfs *RetryFS) OpenFile(filename string, flag int, perm fs.FileMode) (billy.File, error) {
	return retryWithResult(rfs, OpOpenFile, func() (billy.File, error) {
		return rfs.fs.OpenFile(filename, flag, perm)
	})
}

// Stat implements billy.Filesystem
func (rfs *RetryFS) Stat(filename string) (fs.FileInfo, error) {
	return retryWithResult(rfs, OpStat, func() (fs.FileInfo, error) {
		return rfs.fs.Stat(filename)
	})
}

// Rename implements billy.Filesystem
func (rfs *RetryFS) Rename(oldpath, newpath string) error {
	return rfs.retry(OpRename, func() error {
		return rfs.fs.Rename(oldpath, newpath)
	})
}

// Remove implements billy.Filesystem
func (rfs *RetryFS) Remove(filename string) error {
	return rfs.retry(OpRemove, func() error {
		return rfs.fs.Remove(filename)
	})
}

// Join implements billy.Filesystem
func (rfs *RetryFS) Join(elem ...string) string {
	// Join is a pure function, no retry needed
	return rfs.fs.Join(elem...)
}

// TempFile implements billy.Filesystem
func (rfs *RetryFS) TempFile(dir, prefix string) (billy.File, error) {
	return retryWithResult(rfs, OpTempFile, func() (billy.File, error) {
		return rfs.fs.TempFile(dir, prefix)
	})
}

// ReadDir implements billy.Filesystem
func (rfs *RetryFS) ReadDir(path string) ([]fs.FileInfo, error) {
	return retryWithResult(rfs, OpReadDir, func() ([]fs.FileInfo, error) {
		return rfs.fs.ReadDir(path)
	})
}

// MkdirAll implements billy.Filesystem
func (rfs *RetryFS) MkdirAll(filename string, perm fs.FileMode) error {
	return rfs.retry(OpMkdirAll, func() error {
		return rfs.fs.MkdirAll(filename, perm)
	})
}

// Lstat implements billy.Filesystem
func (rfs *RetryFS) Lstat(filename string) (fs.FileInfo, error) {
	return retryWithResult(rfs, OpLstat, func() (fs.FileInfo, error) {
		return rfs.fs.Lstat(filename)
	})
}

// Symlink implements billy.Filesystem
func (rfs *RetryFS) Symlink(target, link string) error {
	return rfs.retry(OpSymlink, func() error {
		return rfs.fs.Symlink(target, link)
	})
}

// Readlink implements billy.Filesystem
func (rfs *RetryFS) Readlink(link string) (string, error) {
	return retryWithResult(rfs, OpReadlink, func() (string, error) {
		return rfs.fs.Readlink(link)
	})
}

// Chroot implements billy.Filesystem
func (rfs *RetryFS) Chroot(path string) (billy.Filesystem, error) {
	// Chroot creates a new filesystem, wrap the result
	return retryWithResult(rfs, OpChroot, func() (billy.Filesystem, error) {
		chrootedFs, err := rfs.fs.Chroot(path)
		if err != nil {
			return nil, err
		}
		// Wrap the chrooted filesystem with retry logic
		return New(chrootedFs, WithConfig(rfs.config)), nil
	})
}

// Root implements billy.Filesystem
func (rfs *RetryFS) Root() string {
	// Root is a pure function, no retry needed
	return rfs.fs.Root()
}
