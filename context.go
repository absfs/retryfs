package retryfs

import (
	"context"
	"os"
	"time"

	"github.com/absfs/absfs"
)

// retryWithContext executes a function with retry logic and context support
func (rfs *RetryFS) retryWithContext(ctx context.Context, op Operation, fn func() error) error {
	// Wrap with circuit breaker if configured
	if rfs.circuitBreaker != nil {
		return rfs.circuitBreaker.Call(func() error {
			return rfs.retryWithoutCircuitBreakerContext(ctx, op, fn)
		})
	}
	return rfs.retryWithoutCircuitBreakerContext(ctx, op, fn)
}

// retryWithoutCircuitBreakerContext executes a function with retry logic and context (without circuit breaker)
func (rfs *RetryFS) retryWithoutCircuitBreakerContext(ctx context.Context, op Operation, fn func() error) error {
	policy := rfs.config.GetPolicy(op)
	var lastErr error

	for attempt := 0; attempt < policy.MaxAttempts; attempt++ {
		// Check context before each attempt
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Record the attempt
		isRetry := attempt > 0
		rfs.mu.Lock()
		rfs.metrics.RecordAttempt(op, isRetry)
		rfs.mu.Unlock()

		// Call the OnRetry callback if this is a retry
		if isRetry && rfs.config.OnRetry != nil {
			rfs.config.OnRetry(op, attempt, lastErr)
		}

		// Sleep before retry (not on first attempt) with context awareness
		if attempt > 0 {
			backoff := rfs.calculateBackoff(policy, attempt)
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
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

// retryWithResultContext executes a function that returns a value and an error with context support
func retryWithResultContext[T any](rfs *RetryFS, ctx context.Context, op Operation, fn func() (T, error)) (T, error) {
	var result T
	var resultErr error

	err := rfs.retryWithContext(ctx, op, func() error {
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

// CreateContext creates a file with context support
func (rfs *RetryFS) CreateContext(ctx context.Context, filename string) (absfs.File, error) {
	return retryWithResultContext(rfs, ctx, OpCreate, func() (absfs.File, error) {
		return rfs.fs.Create(filename)
	})
}

// OpenContext opens a file with context support
func (rfs *RetryFS) OpenContext(ctx context.Context, filename string) (absfs.File, error) {
	return retryWithResultContext(rfs, ctx, OpOpen, func() (absfs.File, error) {
		return rfs.fs.Open(filename)
	})
}

// OpenFileContext opens a file with flags and permissions with context support
func (rfs *RetryFS) OpenFileContext(ctx context.Context, filename string, flag int, perm os.FileMode) (absfs.File, error) {
	return retryWithResultContext(rfs, ctx, OpOpenFile, func() (absfs.File, error) {
		return rfs.fs.OpenFile(filename, flag, perm)
	})
}

// StatContext returns file info with context support
func (rfs *RetryFS) StatContext(ctx context.Context, filename string) (os.FileInfo, error) {
	return retryWithResultContext(rfs, ctx, OpStat, func() (os.FileInfo, error) {
		return rfs.fs.Stat(filename)
	})
}

// RenameContext renames a file with context support
func (rfs *RetryFS) RenameContext(ctx context.Context, oldpath, newpath string) error {
	return rfs.retryWithContext(ctx, OpRename, func() error {
		return rfs.fs.Rename(oldpath, newpath)
	})
}

// RemoveContext removes a file with context support
func (rfs *RetryFS) RemoveContext(ctx context.Context, filename string) error {
	return rfs.retryWithContext(ctx, OpRemove, func() error {
		return rfs.fs.Remove(filename)
	})
}

// MkdirAllContext creates a directory tree with context support
func (rfs *RetryFS) MkdirAllContext(ctx context.Context, filename string, perm os.FileMode) error {
	return rfs.retryWithContext(ctx, OpMkdirAll, func() error {
		return rfs.fs.MkdirAll(filename, perm)
	})
}

// LstatContext returns file info without following symlinks with context support
func (rfs *RetryFS) LstatContext(ctx context.Context, filename string) (os.FileInfo, error) {
	sl, ok := rfs.fs.(absfs.SymLinker)
	if !ok {
		// Fall back to Stat if symlinks not supported
		return rfs.StatContext(ctx, filename)
	}
	return retryWithResultContext(rfs, ctx, OpLstat, func() (os.FileInfo, error) {
		return sl.Lstat(filename)
	})
}

// SymlinkContext creates a symbolic link with context support
func (rfs *RetryFS) SymlinkContext(ctx context.Context, target, link string) error {
	sl, ok := rfs.fs.(absfs.SymLinker)
	if !ok {
		return &os.PathError{Op: "symlink", Path: link, Err: absfs.ErrNotImplemented}
	}
	return rfs.retryWithContext(ctx, OpSymlink, func() error {
		return sl.Symlink(target, link)
	})
}

// ReadlinkContext reads a symbolic link with context support
func (rfs *RetryFS) ReadlinkContext(ctx context.Context, link string) (string, error) {
	sl, ok := rfs.fs.(absfs.SymLinker)
	if !ok {
		return "", &os.PathError{Op: "readlink", Path: link, Err: absfs.ErrNotImplemented}
	}
	return retryWithResultContext(rfs, ctx, OpReadlink, func() (string, error) {
		return sl.Readlink(link)
	})
}

// ChmodContext changes file mode with context support
func (rfs *RetryFS) ChmodContext(ctx context.Context, name string, mode os.FileMode) error {
	return rfs.retryWithContext(ctx, OpChmod, func() error {
		return rfs.fs.Chmod(name, mode)
	})
}

// ChtimesContext changes file times with context support
func (rfs *RetryFS) ChtimesContext(ctx context.Context, name string, atime, mtime time.Time) error {
	return rfs.retryWithContext(ctx, OpChtimes, func() error {
		return rfs.fs.Chtimes(name, atime, mtime)
	})
}

// LchownContext changes file owner without following symlinks with context support
func (rfs *RetryFS) LchownContext(ctx context.Context, name string, uid, gid int) error {
	sl, ok := rfs.fs.(absfs.SymLinker)
	if !ok {
		return &os.PathError{Op: "lchown", Path: name, Err: absfs.ErrNotImplemented}
	}
	return rfs.retryWithContext(ctx, OpLchown, func() error {
		return sl.Lchown(name, uid, gid)
	})
}

// ChownContext changes file owner with context support
func (rfs *RetryFS) ChownContext(ctx context.Context, name string, uid, gid int) error {
	return rfs.retryWithContext(ctx, OpChown, func() error {
		return rfs.fs.Chown(name, uid, gid)
	})
}
