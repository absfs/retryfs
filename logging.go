package retryfs

import (
	"context"
	"log/slog"
	"time"
)

// Logger is the interface for structured logging
type Logger interface {
	// Debug logs a debug message with structured fields
	Debug(msg string, keysAndValues ...interface{})
	// Info logs an info message with structured fields
	Info(msg string, keysAndValues ...interface{})
	// Warn logs a warning message with structured fields
	Warn(msg string, keysAndValues ...interface{})
	// Error logs an error message with structured fields
	Error(msg string, keysAndValues ...interface{})
}

// SlogLogger wraps slog.Logger to implement our Logger interface
type SlogLogger struct {
	logger *slog.Logger
}

// NewSlogLogger creates a Logger from a slog.Logger
func NewSlogLogger(logger *slog.Logger) Logger {
	return &SlogLogger{logger: logger}
}

func (s *SlogLogger) Debug(msg string, keysAndValues ...interface{}) {
	s.logger.Debug(msg, keysAndValues...)
}

func (s *SlogLogger) Info(msg string, keysAndValues ...interface{}) {
	s.logger.Info(msg, keysAndValues...)
}

func (s *SlogLogger) Warn(msg string, keysAndValues ...interface{}) {
	s.logger.Warn(msg, keysAndValues...)
}

func (s *SlogLogger) Error(msg string, keysAndValues ...interface{}) {
	s.logger.Error(msg, keysAndValues...)
}

// NoopLogger is a logger that does nothing
type NoopLogger struct{}

func (n *NoopLogger) Debug(msg string, keysAndValues ...interface{}) {}
func (n *NoopLogger) Info(msg string, keysAndValues ...interface{})  {}
func (n *NoopLogger) Warn(msg string, keysAndValues ...interface{})  {}
func (n *NoopLogger) Error(msg string, keysAndValues ...interface{}) {}

// WithLogger sets a structured logger
func WithLogger(logger Logger) Option {
	return func(rfs *RetryFS) {
		rfs.logger = logger
	}
}

// logRetry logs a retry attempt
func (rfs *RetryFS) logRetry(op Operation, attempt int, err error, delay time.Duration) {
	if rfs.logger == nil {
		return
	}

	rfs.logger.Warn("Retrying filesystem operation",
		"operation", string(op),
		"attempt", attempt,
		"error", err.Error(),
		"delay_ms", delay.Milliseconds(),
	)
}

// logOperationStart logs the start of an operation
func (rfs *RetryFS) logOperationStart(ctx context.Context, op Operation) {
	if rfs.logger == nil {
		return
	}

	rfs.logger.Debug("Starting filesystem operation",
		"operation", string(op),
	)
}

// logOperationComplete logs the completion of an operation
func (rfs *RetryFS) logOperationComplete(op Operation, attempts int, err error, duration time.Duration) {
	if rfs.logger == nil {
		return
	}

	if err != nil {
		rfs.logger.Error("Filesystem operation failed",
			"operation", string(op),
			"attempts", attempts,
			"error", err.Error(),
			"duration_ms", duration.Milliseconds(),
		)
	} else {
		rfs.logger.Debug("Filesystem operation completed",
			"operation", string(op),
			"attempts", attempts,
			"duration_ms", duration.Milliseconds(),
		)
	}
}

// logCircuitBreakerStateChange logs circuit breaker state changes
func logCircuitBreakerStateChange(logger Logger, from, to State) {
	if logger == nil {
		return
	}

	logger.Warn("Circuit breaker state changed",
		"from", from.String(),
		"to", to.String(),
	)
}

// logPerOperationCircuitBreakerStateChange logs per-operation circuit breaker state changes
func logPerOperationCircuitBreakerStateChange(logger Logger, op Operation, from, to State) {
	if logger == nil {
		return
	}

	logger.Warn("Per-operation circuit breaker state changed",
		"operation", string(op),
		"from", from.String(),
		"to", to.String(),
	)
}
