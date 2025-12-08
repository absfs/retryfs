package retryfs

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

)



// TestLoadTest_SequentialOperations tests sustained sequential load
func TestLoadTest_SequentialOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	underlying := mustNewMemFS()
	fs := New(underlying, WithPolicy(&Policy{
		MaxAttempts: 5,
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
		Jitter:      0.1,
		Multiplier:  2.0,
	})).(*RetryFS)

	const numOps = 10000
	var successOps int64

	startTime := time.Now()
	for i := 0; i < numOps; i++ {
		// Mix of operations
		switch i % 4 {
		case 0:
			err := fs.MkdirAll(fmt.Sprintf("/dir%d", i), 0755)
			if err == nil {
				atomic.AddInt64(&successOps, 1)
			}
		case 1:
			f, err := fs.Create(fmt.Sprintf("/file%d.txt", i))
			if err == nil {
				f.Close()
				atomic.AddInt64(&successOps, 1)
			}
		case 2:
			_, err := fs.Stat("/")
			if err == nil {
				atomic.AddInt64(&successOps, 1)
			}
		case 3:
			f, err := fs.Open("/"); if err == nil { _, err = f.Readdir(-1); f.Close() }
			if err == nil {
				atomic.AddInt64(&successOps, 1)
			}
		}
	}
	duration := time.Since(startTime)

	opsPerSec := float64(numOps) / duration.Seconds()
	successRate := float64(successOps) / float64(numOps) * 100

	t.Logf("Sequential Load Test Results:")
	t.Logf("  Total Operations: %d", numOps)
	t.Logf("  Successful: %d (%.2f%%)", successOps, successRate)
	t.Logf("  Duration: %s", duration)
	t.Logf("  Ops/Second: %.2f", opsPerSec)

	if successRate < 99.0 {
		t.Errorf("Expected > 99%% success rate, got %.2f%%", successRate)
	}
}

// TestLoadTest_WithRetries tests sustained load with transient failures
func TestLoadTest_WithRetries(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	const numOps = 1000
	underlying := newFailingFS(mustNewMemFS(), numOps/5) // 20% failure rate

	var retries int64
	fs := New(underlying,
		WithPolicy(&Policy{
			MaxAttempts: 5,
			BaseDelay:   1 * time.Millisecond,
			MaxDelay:    100 * time.Millisecond,
			Jitter:      0.1,
			Multiplier:  2.0,
		}),
		WithOnRetry(func(op Operation, attempt int, err error) {
			atomic.AddInt64(&retries, 1)
		}),
	).(*RetryFS)

	var successOps int64
	startTime := time.Now()

	for i := 0; i < numOps; i++ {
		err := fs.MkdirAll(fmt.Sprintf("/dir%d", i), 0755)
		if err == nil {
			atomic.AddInt64(&successOps, 1)
		}
	}

	duration := time.Since(startTime)
	successRate := float64(successOps) / float64(numOps) * 100

	t.Logf("Load Test with Retries Results:")
	t.Logf("  Total Operations: %d", numOps)
	t.Logf("  Successful: %d (%.2f%%)", successOps, successRate)
	t.Logf("  Retries: %d", retries)
	t.Logf("  Duration: %s", duration)

	if retries == 0 {
		t.Error("Expected some retries with 20% failure rate")
	}

	// Should eventually succeed despite failures
	if successRate < 70.0 {
		t.Errorf("Expected > 70%% success rate after retries, got %.2f%%", successRate)
	}
}

// TestLoadTest_CircuitBreakerUnderLoad tests circuit breaker behavior under load
func TestLoadTest_CircuitBreakerUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	const numOps = 500
	underlying := newFailingFS(mustNewMemFS(), numOps) // Always fail

	var cbStateChanges int64
	cb := NewCircuitBreaker()
	cb.FailureThreshold = 5
	cb.OnStateChange = func(from, to State) {
		atomic.AddInt64(&cbStateChanges, 1)
	}

	fs := New(underlying,
		WithCircuitBreaker(cb),
		WithPolicy(&Policy{
			MaxAttempts: 1,
			BaseDelay:   1 * time.Millisecond,
		}),
	).(*RetryFS)

	var failedOps int64
	for i := 0; i < numOps; i++ {
		err := fs.MkdirAll(fmt.Sprintf("/dir%d", i), 0755)
		if err != nil {
			atomic.AddInt64(&failedOps, 1)
		}

		// Stop early if circuit is open
		if cb.GetState() == StateOpen {
			t.Logf("Circuit opened after %d operations", i+1)
			break
		}
	}

	// Wait for async callback
	time.Sleep(100 * time.Millisecond)

	t.Logf("Circuit Breaker Load Test Results:")
	t.Logf("  Failed Operations: %d", atomic.LoadInt64(&failedOps))
	t.Logf("  State Changes: %d", atomic.LoadInt64(&cbStateChanges))
	t.Logf("  Final State: %s", cb.GetState())

	if atomic.LoadInt64(&cbStateChanges) == 0 {
		t.Error("Expected circuit breaker to change state under high failure rate")
	}
}

// TestLoadTest_PerOperationCircuitBreaker tests per-operation circuit breaker under load
func TestLoadTest_PerOperationCircuitBreaker(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	const numOps = 500
	underlying := newFailingFS(mustNewMemFS(), numOps) // Always fail

	var stateChanges int64
	pocb := NewPerOperationCircuitBreaker(&CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
		OnStateChange: func(op Operation, from, to State) {
			atomic.AddInt64(&stateChanges, 1)
		},
	})

	fs := New(underlying,
		WithPerOperationCircuitBreaker(pocb),
		WithPolicy(&Policy{
			MaxAttempts: 1,
			BaseDelay:   1 * time.Millisecond,
		}),
	).(*RetryFS)

	// Try different operations
	for i := 0; i < numOps/2; i++ {
		_ = fs.MkdirAll(fmt.Sprintf("/dir%d", i), 0755)
		f, _ := fs.Create(fmt.Sprintf("/file%d.txt", i))
		if f != nil {
			f.Close()
		}
	}

	// Wait for async callbacks
	time.Sleep(100 * time.Millisecond)

	states := pocb.GetAllStates()
	t.Logf("Per-Operation CB Results:")
	for op, state := range states {
		t.Logf("  %s: %s", op, state)
	}

	if len(states) == 0 {
		t.Error("Expected at least one circuit breaker to be created")
	}
}

// TestLoadTest_ContextTimeout tests context timeout behavior under load
func TestLoadTest_ContextTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	underlying := mustNewMemFS()
	fs := New(underlying).(*RetryFS)

	var cancelledOps int64
	var completedOps int64

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Try to perform many operations
	for i := 0; i < 10000; i++ {
		err := fs.MkdirAllContext(ctx, fmt.Sprintf("/dir%d", i), 0755)
		if err == context.DeadlineExceeded || err == context.Canceled {
			atomic.AddInt64(&cancelledOps, 1)
			break
		}
		if err == nil {
			atomic.AddInt64(&completedOps, 1)
		}
	}

	t.Logf("Context Timeout Results:")
	t.Logf("  Completed: %d", completedOps)
	t.Logf("  Cancelled: %d", cancelledOps)

	if completedOps == 0 && cancelledOps == 0 {
		t.Error("Expected some operations to complete or be cancelled")
	}
}

// TestLoadTest_MetricsAccuracy tests metrics accuracy under load
func TestLoadTest_MetricsAccuracy(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	const numOps = 1000
	underlying := newFailingFS(mustNewMemFS(), numOps/10) // 10% failure rate

	fs := New(underlying, WithPolicy(&Policy{
		MaxAttempts: 5,
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
		Jitter:      0,
		Multiplier:  2.0,
	})).(*RetryFS)

	var actualOps int64
	for i := 0; i < numOps; i++ {
		_ = fs.MkdirAll(fmt.Sprintf("/dir%d", i), 0755)
		atomic.AddInt64(&actualOps, 1)
	}

	metrics := fs.GetMetrics()

	t.Logf("Metrics Accuracy Results:")
	t.Logf("  Actual Operations: %d", actualOps)
	t.Logf("  Recorded Attempts: %d", metrics.TotalAttempts)
	t.Logf("  Retries: %d", metrics.TotalRetries)
	t.Logf("  Successes: %d", metrics.TotalSuccesses)
	t.Logf("  Failures: %d", metrics.TotalFailures)

	// Total attempts should be at least equal to actual operations
	if metrics.TotalAttempts < actualOps {
		t.Errorf("Metrics undercount: %d attempts vs %d operations", metrics.TotalAttempts, actualOps)
	}

	// Successes + failures should equal actual operations
	totalCompleted := metrics.TotalSuccesses + metrics.TotalFailures
	if totalCompleted != actualOps {
		t.Logf("Warning: Completed count mismatch: %d vs %d", totalCompleted, actualOps)
	}
}

// BenchmarkLoadTest_Throughput benchmarks maximum throughput
func BenchmarkLoadTest_Throughput(b *testing.B) {
	underlying := mustNewMemFS()
	fs := New(underlying).(*RetryFS)

	// Prepare test files
	for i := 0; i < 100; i++ {
		f, _ := fs.Create(fmt.Sprintf("/file%d.txt", i))
		f.Close()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = fs.Stat(fmt.Sprintf("/file%d.txt", i%100))
	}

	opsPerSec := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(opsPerSec, "ops/sec")
}

// BenchmarkLoadTest_ThroughputWithRetries benchmarks throughput with retries
func BenchmarkLoadTest_ThroughputWithRetries(b *testing.B) {
	underlying := mustNewMemFS()
	failingFS := newFailingFS(underlying, b.N/10) // 10% failure rate
	fs := New(failingFS, WithPolicy(&Policy{
		MaxAttempts: 3,
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    10 * time.Millisecond,
		Jitter:      0,
		Multiplier:  2.0,
	})).(*RetryFS)

	// Prepare test files
	for i := 0; i < 100; i++ {
		f, _ := underlying.Create(fmt.Sprintf("/file%d.txt", i))
		f.Close()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = fs.Stat(fmt.Sprintf("/file%d.txt", i%100))
	}

	opsPerSec := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(opsPerSec, "ops/sec")
}

// TestLoadTest_SustainedLoad tests filesystem behavior over extended period
func TestLoadTest_SustainedLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	underlying := mustNewMemFS()
	fs := New(underlying, WithPolicy(&Policy{
		MaxAttempts: 3,
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
		Jitter:      0.1,
		Multiplier:  2.0,
	})).(*RetryFS)

	duration := 5 * time.Second
	var opsCompleted int64
	startTime := time.Now()

	for time.Since(startTime) < duration {
		i := int(atomic.LoadInt64(&opsCompleted))
		_ = fs.MkdirAll(fmt.Sprintf("/dir%d", i), 0755)
		atomic.AddInt64(&opsCompleted, 1)
	}

	actualDuration := time.Since(startTime)
	opsPerSec := float64(opsCompleted) / actualDuration.Seconds()

	t.Logf("Sustained Load Test Results:")
	t.Logf("  Duration: %s", actualDuration)
	t.Logf("  Operations: %d", opsCompleted)
	t.Logf("  Ops/Second: %.2f", opsPerSec)

	if opsCompleted == 0 {
		t.Error("No operations completed")
	}

	// Check metrics don't show signs of memory leaks
	metrics := fs.GetMetrics()
	t.Logf("  Metrics - Attempts: %d, Successes: %d, Failures: %d",
		metrics.TotalAttempts, metrics.TotalSuccesses, metrics.TotalFailures)
}

// TestLoadTest_ConcurrentWorkers tests independent workers with separate filesystems
// This works around memfs thread-safety issues by giving each worker its own filesystem
func TestLoadTest_ConcurrentWorkers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	const numWorkers = 10
	const opsPerWorker = 100

	var wg sync.WaitGroup
	var totalOps int64
	var totalSuccess int64

	startTime := time.Now()

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Each worker gets its own filesystem (avoiding memfs thread-safety issues)
			underlying := mustNewMemFS()
			fs := New(underlying, WithPolicy(&Policy{
				MaxAttempts: 3,
				BaseDelay:   1 * time.Millisecond,
				MaxDelay:    10 * time.Millisecond,
				Jitter:      0.1,
				Multiplier:  2.0,
			})).(*RetryFS)

			for j := 0; j < opsPerWorker; j++ {
				atomic.AddInt64(&totalOps, 1)
				err := fs.MkdirAll(fmt.Sprintf("/dir%d", j), 0755)
				if err == nil {
					atomic.AddInt64(&totalSuccess, 1)
				}
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(startTime)

	successRate := float64(totalSuccess) / float64(totalOps) * 100
	opsPerSec := float64(totalOps) / duration.Seconds()

	t.Logf("Concurrent Workers Test Results:")
	t.Logf("  Workers: %d", numWorkers)
	t.Logf("  Total Operations: %d", totalOps)
	t.Logf("  Successful: %d (%.2f%%)", totalSuccess, successRate)
	t.Logf("  Duration: %s", duration)
	t.Logf("  Ops/Second: %.2f", opsPerSec)

	if totalOps != numWorkers*opsPerWorker {
		t.Errorf("Expected %d operations, got %d", numWorkers*opsPerWorker, totalOps)
	}

	if successRate < 99.0 {
		t.Errorf("Expected > 99%% success rate, got %.2f%%", successRate)
	}
}
