package retryfs

import (
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"strings"
	"syscall"
)

// defaultErrorClassifier classifies common filesystem errors
func defaultErrorClassifier(err error) ErrorClass {
	if err == nil {
		return ErrorUnknown
	}

	// Permanent errors that should not be retried
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return ErrorPermanent
	case errors.Is(err, fs.ErrExist):
		return ErrorPermanent
	case errors.Is(err, fs.ErrPermission):
		return ErrorPermanent
	case errors.Is(err, fs.ErrInvalid):
		return ErrorPermanent
	case errors.Is(err, io.EOF):
		return ErrorPermanent
	case errors.Is(err, io.ErrUnexpectedEOF):
		return ErrorRetryable
	case errors.Is(err, io.ErrShortWrite):
		return ErrorRetryable
	case errors.Is(err, io.ErrClosedPipe):
		return ErrorRetryable
	}

	// Check for syscall errors
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return classifySyscallError(errno)
	}

	// Check for network errors
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() || netErr.Temporary() {
			return ErrorRetryable
		}
	}

	// Check for OS-specific errors
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return defaultErrorClassifier(pathErr.Err)
	}

	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return defaultErrorClassifier(linkErr.Err)
	}

	// Check error message for common patterns
	errMsg := err.Error()
	if classifyByMessage(errMsg) != ErrorUnknown {
		return classifyByMessage(errMsg)
	}

	// Default to unknown for unrecognized errors
	// The retry logic can be configured to retry unknown errors conservatively
	return ErrorUnknown
}

// classifySyscallError classifies syscall errors
func classifySyscallError(errno syscall.Errno) ErrorClass {
	switch errno {
	// Permanent errors
	case syscall.ENOENT: // No such file or directory
		return ErrorPermanent
	case syscall.EEXIST: // File exists
		return ErrorPermanent
	case syscall.EACCES: // Permission denied
		return ErrorPermanent
	case syscall.EPERM: // Operation not permitted
		return ErrorPermanent
	case syscall.EINVAL: // Invalid argument
		return ErrorPermanent
	case syscall.EISDIR: // Is a directory
		return ErrorPermanent
	case syscall.ENOTDIR: // Not a directory
		return ErrorPermanent
	case syscall.ENOTEMPTY: // Directory not empty
		return ErrorPermanent
	case syscall.EROFS: // Read-only file system
		return ErrorPermanent
	case syscall.ENOSPC: // No space left on device
		return ErrorPermanent
	case syscall.EDQUOT: // Disk quota exceeded
		return ErrorPermanent

	// Retryable errors
	case syscall.EAGAIN: // Resource temporarily unavailable (also EWOULDBLOCK)
		return ErrorRetryable
	case syscall.EINTR: // Interrupted system call
		return ErrorRetryable
	case syscall.EIO: // I/O error
		return ErrorRetryable
	case syscall.ENFILE: // Too many open files in system
		return ErrorRetryable
	case syscall.EMFILE: // Too many open files
		return ErrorRetryable
	case syscall.ETIMEDOUT: // Connection timed out
		return ErrorRetryable
	case syscall.ECONNREFUSED: // Connection refused
		return ErrorRetryable
	case syscall.ECONNRESET: // Connection reset
		return ErrorRetryable
	case syscall.ECONNABORTED: // Connection aborted
		return ErrorRetryable
	case syscall.ENETUNREACH: // Network unreachable
		return ErrorRetryable
	case syscall.EHOSTUNREACH: // Host unreachable
		return ErrorRetryable
	case syscall.EPIPE: // Broken pipe
		return ErrorRetryable

	default:
		return ErrorUnknown
	}
}

// classifyByMessage attempts to classify errors by their message content
func classifyByMessage(msg string) ErrorClass {
	msgLower := strings.ToLower(msg)

	// Retryable patterns
	retryablePatterns := []string{
		"timeout",
		"timed out",
		"temporary failure",
		"temporarily unavailable",
		"connection refused",
		"connection reset",
		"broken pipe",
		"too many open files",
		"resource temporarily unavailable",
		"503",
		"service unavailable",
		"throttl", // matches "throttled", "throttling"
		"rate limit",
		"try again",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(msgLower, pattern) {
			return ErrorRetryable
		}
	}

	// Permanent patterns
	permanentPatterns := []string{
		"not found",
		"404",
		"does not exist",
		"permission denied",
		"403",
		"unauthorized",
		"401",
		"invalid argument",
		"400",
		"bad request",
		"not supported",
		"not implemented",
		"quota exceeded",
		"no space left",
	}

	for _, pattern := range permanentPatterns {
		if strings.Contains(msgLower, pattern) {
			return ErrorPermanent
		}
	}

	return ErrorUnknown
}

// IsRetryable is a helper function to determine if an error is retryable
// using the default classifier
func IsRetryable(err error) bool {
	return defaultErrorClassifier(err) == ErrorRetryable
}

// IsPermanent is a helper function to determine if an error is permanent
// using the default classifier
func IsPermanent(err error) bool {
	return defaultErrorClassifier(err) == ErrorPermanent
}
