package retryfs

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
)

func TestSlogLogger(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	slogger := slog.New(handler)
	logger := NewSlogLogger(slogger)

	logger.Debug("debug message", "key", "value")
	logger.Info("info message", "key", "value")
	logger.Warn("warn message", "key", "value")
	logger.Error("error message", "key", "value")

	output := buf.String()

	if !strings.Contains(output, "debug message") {
		t.Error("Expected debug message in output")
	}
	if !strings.Contains(output, "info message") {
		t.Error("Expected info message in output")
	}
	if !strings.Contains(output, "warn message") {
		t.Error("Expected warn message in output")
	}
	if !strings.Contains(output, "error message") {
		t.Error("Expected error message in output")
	}
	if !strings.Contains(output, "key=value") {
		t.Error("Expected structured field in output")
	}
}

func TestNoopLogger(t *testing.T) {
	logger := &NoopLogger{}

	// Should not panic
	logger.Debug("test", "key", "value")
	logger.Info("test", "key", "value")
	logger.Warn("test", "key", "value")
	logger.Error("test", "key", "value")
}

func TestRetryFS_WithLogger(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	slogger := slog.New(handler)
	logger := NewSlogLogger(slogger)

	underlying := memfs.New()
	fs := New(underlying, WithLogger(logger)).(*RetryFS)

	if fs.logger == nil {
		t.Fatal("Expected logger to be set")
	}

	// Perform an operation
	_ = fs.MkdirAll("/test", 0755)

	output := buf.String()
	if !strings.Contains(output, "operation") {
		t.Error("Expected operation field in log output")
	}
}

func TestRetryFS_LoggingWithRetries(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	slogger := slog.New(handler)
	logger := NewSlogLogger(slogger)

	// Create filesystem that fails twice
	failing := newFailingFS(memfs.New(), 2)
	fs := New(failing,
		WithLogger(logger),
		WithPolicy(&Policy{
			MaxAttempts: 5,
			BaseDelay:   1,
			MaxDelay:    10,
			Jitter:      0,
			Multiplier:  1,
		}),
	).(*RetryFS)

	// Trigger retries
	_, _ = fs.Open("/nonexistent")

	output := buf.String()

	// Should log retry attempts
	if !strings.Contains(output, "Retrying") || !strings.Contains(output, "attempt") {
		t.Error("Expected retry log messages")
	}
}

func TestRetryFS_LoggingWithCircuitBreaker(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	slogger := slog.New(handler)
	logger := NewSlogLogger(slogger)

	cb := NewCircuitBreaker()
	cb.FailureThreshold = 2
	cb.OnStateChange = func(from, to State) {
		logCircuitBreakerStateChange(logger, from, to)
	}

	underlying := newFailingFS(memfs.New(), 10) // Always fail
	fs := New(underlying,
		WithLogger(logger),
		WithCircuitBreaker(cb),
		WithPolicy(&Policy{
			MaxAttempts: 1,
			BaseDelay:   1,
		}),
	).(*RetryFS)

	// Cause failures to open circuit
	for i := 0; i < 5; i++ {
		_ = fs.MkdirAll("/test", 0755)
	}

	// Wait for async OnStateChange callback to complete
	time.Sleep(50 * time.Millisecond)

	output := buf.String()

	// Should log circuit breaker state changes
	if !strings.Contains(output, "Circuit breaker") || !strings.Contains(output, "state") {
		t.Log("Output:", output)
		t.Error("Expected circuit breaker state change logs")
	}
}

func TestRetryFS_LoggingWithPerOperationCircuitBreaker(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	slogger := slog.New(handler)
	logger := NewSlogLogger(slogger)

	pocb := NewPerOperationCircuitBreaker(&CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          100,
		OnStateChange: func(op Operation, from, to State) {
			logPerOperationCircuitBreakerStateChange(logger, op, from, to)
		},
	})

	underlying := newFailingFS(memfs.New(), 10) // Always fail
	fs := New(underlying,
		WithLogger(logger),
		WithPerOperationCircuitBreaker(pocb),
		WithPolicy(&Policy{
			MaxAttempts: 1,
			BaseDelay:   1,
		}),
	).(*RetryFS)

	// Cause failures to open circuit
	for i := 0; i < 5; i++ {
		_ = fs.MkdirAll("/test", 0755)
	}

	// Wait for async OnStateChange callback to complete
	time.Sleep(50 * time.Millisecond)

	output := buf.String()

	// Should log per-operation circuit breaker state changes
	if !strings.Contains(output, "Per-operation circuit breaker") {
		t.Log("Output:", output)
		t.Error("Expected per-operation circuit breaker state change logs")
	}
}

func TestRetryFS_WithoutLogger(t *testing.T) {
	underlying := memfs.New()
	fs := New(underlying).(*RetryFS)

	// Should not panic when logger is nil
	_ = fs.MkdirAll("/test", 0755)

	// Try to trigger retry logging
	failing := newFailingFS(underlying, 1)
	fs2 := New(failing).(*RetryFS)
	_, _ = fs2.Open("/nonexistent")
}

// customLoggerImpl implementation
type customLoggerImpl struct {
	debugCount int
	infoCount  int
	warnCount  int
	errorCount int
}

func (c *customLoggerImpl) Debug(msg string, keysAndValues ...interface{}) {
	c.debugCount++
}

func (c *customLoggerImpl) Info(msg string, keysAndValues ...interface{}) {
	c.infoCount++
}

func (c *customLoggerImpl) Warn(msg string, keysAndValues ...interface{}) {
	c.warnCount++
}

func (c *customLoggerImpl) Error(msg string, keysAndValues ...interface{}) {
	c.errorCount++
}

// TestCustomLogger tests that custom logger implementations work
func TestCustomLogger(t *testing.T) {
	// Verify interface is implemented
	var _ Logger = (*customLoggerImpl)(nil)

	logger := &customLoggerImpl{}
	logger.Debug("test", "key", "value")
	logger.Info("test", "key", "value")
	logger.Warn("test", "key", "value")
	logger.Error("test", "key", "value")

	if logger.debugCount != 1 {
		t.Errorf("Expected 1 debug call, got %d", logger.debugCount)
	}
	if logger.infoCount != 1 {
		t.Errorf("Expected 1 info call, got %d", logger.infoCount)
	}
	if logger.warnCount != 1 {
		t.Errorf("Expected 1 warn call, got %d", logger.warnCount)
	}
	if logger.errorCount != 1 {
		t.Errorf("Expected 1 error call, got %d", logger.errorCount)
	}
}

func TestLogCircuitBreakerStateChange_NilLogger(t *testing.T) {
	// Should not panic with nil logger
	logCircuitBreakerStateChange(nil, StateClosed, StateOpen)
}

func TestLogPerOperationCircuitBreakerStateChange_NilLogger(t *testing.T) {
	// Should not panic with nil logger
	logPerOperationCircuitBreakerStateChange(nil, OpOpen, StateClosed, StateOpen)
}
