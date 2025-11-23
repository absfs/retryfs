package retryfs

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
)

// BenchmarkOpen_NoRetry benchmarks open operation on unwrapped filesystem
func BenchmarkOpen_NoRetry(b *testing.B) {
	fs := memfs.New()

	// Create a test file
	f, _ := fs.Create("/test.txt")
	f.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f, err := fs.Open("/test.txt")
		if err != nil {
			b.Fatal(err)
		}
		f.Close()
	}
}

// BenchmarkOpen_WithRetryWrapper benchmarks open with retry wrapper (no retries needed)
func BenchmarkOpen_WithRetryWrapper(b *testing.B) {
	underlying := memfs.New()
	fs := New(underlying).(*RetryFS)

	// Create a test file
	f, _ := fs.Create("/test.txt")
	f.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f, err := fs.Open("/test.txt")
		if err != nil {
			b.Fatal(err)
		}
		f.Close()
	}
}

// BenchmarkOpen_WithRetry_1Fail benchmarks open with 1 transient failure
func BenchmarkOpen_WithRetry_1Fail(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		underlying := newFailingFS(memfs.New(), 1) // Fail once
		fs := New(underlying, WithPolicy(&Policy{
			MaxAttempts: 3,
			BaseDelay:   1 * time.Millisecond,
			MaxDelay:    10 * time.Millisecond,
			Jitter:      0,
			Multiplier:  2.0,
		})).(*RetryFS)

		// Create a test file
		f, _ := underlying.fs.Create("/test.txt")
		f.Close()
		b.StartTimer()

		f, err := fs.Open("/test.txt")
		if err != nil {
			b.Fatal(err)
		}
		f.Close()
	}
}

// BenchmarkOpen_WithRetry_3Fail benchmarks open with 3 transient failures
func BenchmarkOpen_WithRetry_3Fail(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		underlying := newFailingFS(memfs.New(), 3) // Fail 3 times
		fs := New(underlying, WithPolicy(&Policy{
			MaxAttempts: 5,
			BaseDelay:   1 * time.Millisecond,
			MaxDelay:    10 * time.Millisecond,
			Jitter:      0,
			Multiplier:  2.0,
		})).(*RetryFS)

		// Create a test file
		f, _ := underlying.fs.Create("/test.txt")
		f.Close()
		b.StartTimer()

		f, err := fs.Open("/test.txt")
		if err != nil {
			b.Fatal(err)
		}
		f.Close()
	}
}

// BenchmarkCalculateBackoff benchmarks backoff calculation
func BenchmarkCalculateBackoff(b *testing.B) {
	fs := New(memfs.New()).(*RetryFS)
	policy := DefaultPolicy

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fs.calculateBackoff(policy, i%10) // Test attempts 0-9
	}
}

// BenchmarkCircuitBreaker_Closed benchmarks circuit breaker in closed state
func BenchmarkCircuitBreaker_Closed(b *testing.B) {
	cb := NewCircuitBreaker()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := cb.Call(func() error {
			return nil // Always succeed
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCircuitBreaker_Open benchmarks circuit breaker in open state
func BenchmarkCircuitBreaker_Open(b *testing.B) {
	cb := NewCircuitBreaker()
	cb.FailureThreshold = 1

	// Open the circuit
	cb.Call(func() error { return errors.New("fail") })

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cb.Call(func() error {
			return nil
		})
	}
}

// BenchmarkStat_NoRetry benchmarks stat without retry wrapper
func BenchmarkStat_NoRetry(b *testing.B) {
	fs := memfs.New()

	// Create a test file
	f, _ := fs.Create("/test.txt")
	f.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := fs.Stat("/test.txt")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStat_WithRetryWrapper benchmarks stat with retry wrapper
func BenchmarkStat_WithRetryWrapper(b *testing.B) {
	underlying := memfs.New()
	fs := New(underlying).(*RetryFS)

	// Create a test file
	f, _ := fs.Create("/test.txt")
	f.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := fs.Stat("/test.txt")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMkdirAll_NoRetry benchmarks mkdir without retry wrapper
func BenchmarkMkdirAll_NoRetry(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		fs := memfs.New()
		b.StartTimer()

		err := fs.MkdirAll("/test/deep/path", 0755)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMkdirAll_WithRetryWrapper benchmarks mkdir with retry wrapper
func BenchmarkMkdirAll_WithRetryWrapper(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		underlying := memfs.New()
		fs := New(underlying).(*RetryFS)
		b.StartTimer()

		err := fs.MkdirAll("/test/deep/path", 0755)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkContext_Success benchmarks context-aware operation (success case)
func BenchmarkContext_Success(b *testing.B) {
	underlying := memfs.New()
	fs := New(underlying).(*RetryFS)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fs.MkdirAllContext(ctx, "/test", 0755)
		// Ignore ErrExist since we're creating the same directory multiple times
	}
}

// BenchmarkContext_WithDeadline benchmarks context-aware operation with deadline
func BenchmarkContext_WithDeadline(b *testing.B) {
	underlying := memfs.New()
	fs := New(underlying).(*RetryFS)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		_ = fs.MkdirAllContext(ctx, "/test", 0755)
		cancel()
		// Ignore ErrExist since we're creating the same directory multiple times
	}
}

// BenchmarkErrorClassifier benchmarks error classification
func BenchmarkErrorClassifier(b *testing.B) {
	testErrors := []error{
		fs.ErrNotExist,
		fs.ErrPermission,
		errors.New("connection timeout"),
		errors.New("500 internal server error"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = defaultErrorClassifier(testErrors[i%len(testErrors)])
	}
}

// BenchmarkMetricsRecording benchmarks metrics recording overhead
func BenchmarkMetricsRecording(b *testing.B) {
	metrics := NewMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics.RecordAttempt(OpOpen, false)
		metrics.RecordSuccess(OpOpen)
	}
}

// BenchmarkParallelOpen benchmarks concurrent open operations
func BenchmarkParallelOpen(b *testing.B) {
	underlying := memfs.New()
	fs := New(underlying).(*RetryFS)

	// Create a test file
	f, _ := fs.Create("/test.txt")
	f.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			f, err := fs.Open("/test.txt")
			if err != nil {
				b.Fatal(err)
			}
			f.Close()
		}
	})
}

// BenchmarkParallelStat benchmarks concurrent stat operations
func BenchmarkParallelStat(b *testing.B) {
	underlying := memfs.New()
	fs := New(underlying).(*RetryFS)

	// Create a test file
	f, _ := fs.Create("/test.txt")
	f.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := fs.Stat("/test.txt")
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
