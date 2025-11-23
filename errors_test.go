package retryfs

import (
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"syscall"
	"testing"
)

func TestDefaultErrorClassifier(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected ErrorClass
	}{
		// Permanent errors
		{"nil error", nil, ErrorUnknown},
		{"ErrNotExist", fs.ErrNotExist, ErrorPermanent},
		{"ErrExist", fs.ErrExist, ErrorPermanent},
		{"ErrPermission", fs.ErrPermission, ErrorPermanent},
		{"ErrInvalid", fs.ErrInvalid, ErrorPermanent},
		{"EOF", io.EOF, ErrorPermanent},

		// Retryable errors
		{"ErrUnexpectedEOF", io.ErrUnexpectedEOF, ErrorRetryable},
		{"ErrShortWrite", io.ErrShortWrite, ErrorRetryable},
		{"ErrClosedPipe", io.ErrClosedPipe, ErrorRetryable},

		// Syscall errors - permanent
		{"ENOENT", syscall.ENOENT, ErrorPermanent},
		{"EACCES", syscall.EACCES, ErrorPermanent},
		{"EPERM", syscall.EPERM, ErrorPermanent},
		{"EINVAL", syscall.EINVAL, ErrorPermanent},
		{"EISDIR", syscall.EISDIR, ErrorPermanent},
		{"ENOTDIR", syscall.ENOTDIR, ErrorPermanent},
		{"EROFS", syscall.EROFS, ErrorPermanent},

		// Syscall errors - retryable
		{"EAGAIN", syscall.EAGAIN, ErrorRetryable},
		{"EINTR", syscall.EINTR, ErrorRetryable},
		{"EIO", syscall.EIO, ErrorRetryable},
		{"ETIMEDOUT", syscall.ETIMEDOUT, ErrorRetryable},
		{"ECONNREFUSED", syscall.ECONNREFUSED, ErrorRetryable},
		{"EPIPE", syscall.EPIPE, ErrorRetryable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := defaultErrorClassifier(tt.err)
			if result != tt.expected {
				t.Errorf("defaultErrorClassifier(%v) = %v, want %v",
					tt.err, result, tt.expected)
			}
		})
	}
}

func TestClassifyByMessage(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected ErrorClass
	}{
		// Retryable patterns
		{"timeout", "connection timeout", ErrorRetryable},
		{"timed out", "operation timed out", ErrorRetryable},
		{"connection refused", "connection refused", ErrorRetryable},
		{"503", "503 Service Unavailable", ErrorRetryable},
		{"throttled", "request throttled", ErrorRetryable},
		{"rate limit", "rate limit exceeded", ErrorRetryable},

		// Permanent patterns
		{"not found", "file not found", ErrorPermanent},
		{"404", "404 Not Found", ErrorPermanent},
		{"permission denied", "permission denied", ErrorPermanent},
		{"403", "403 Forbidden", ErrorPermanent},
		{"401", "401 Unauthorized", ErrorPermanent},
		{"invalid argument", "invalid argument", ErrorPermanent},
		{"quota exceeded", "quota exceeded", ErrorPermanent},

		// Unknown
		{"unrecognized", "some random error", ErrorUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyByMessage(tt.message)
			if result != tt.expected {
				t.Errorf("classifyByMessage(%q) = %v, want %v",
					tt.message, result, tt.expected)
			}
		})
	}
}

func TestPathErrorWrapping(t *testing.T) {
	pathErr := &os.PathError{
		Op:   "open",
		Path: "/tmp/file",
		Err:  syscall.ENOENT,
	}

	result := defaultErrorClassifier(pathErr)
	if result != ErrorPermanent {
		t.Errorf("Expected permanent error for ENOENT wrapped in PathError, got %v", result)
	}
}

func TestNetworkError(t *testing.T) {
	// Create a mock network timeout error
	timeoutErr := &mockNetError{timeout: true, temporary: false}
	result := defaultErrorClassifier(timeoutErr)
	if result != ErrorRetryable {
		t.Errorf("Expected retryable error for network timeout, got %v", result)
	}

	temporaryErr := &mockNetError{timeout: false, temporary: true}
	result = defaultErrorClassifier(temporaryErr)
	if result != ErrorRetryable {
		t.Errorf("Expected retryable error for temporary network error, got %v", result)
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil", nil, false},
		{"retryable", syscall.ETIMEDOUT, true},
		{"permanent", fs.ErrNotExist, false},
		{"unknown", errors.New("random error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsRetryable(tt.err)
			if result != tt.expected {
				t.Errorf("IsRetryable(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestIsPermanent(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil", nil, false},
		{"permanent", fs.ErrNotExist, true},
		{"retryable", syscall.ETIMEDOUT, false},
		{"unknown", errors.New("random error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsPermanent(tt.err)
			if result != tt.expected {
				t.Errorf("IsPermanent(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

// mockNetError implements net.Error for testing
type mockNetError struct {
	timeout   bool
	temporary bool
}

func (e *mockNetError) Error() string   { return "mock network error" }
func (e *mockNetError) Timeout() bool   { return e.timeout }
func (e *mockNetError) Temporary() bool { return e.temporary }

var _ net.Error = (*mockNetError)(nil)
