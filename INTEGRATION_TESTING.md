# Integration Testing with RetryFS

This guide covers integration testing patterns and best practices when using RetryFS in real-world scenarios.

## Table of Contents

- [Overview](#overview)
- [Testing Strategies](#testing-strategies)
- [Testing with Real Filesystems](#testing-with-real-filesystems)
- [Mocking and Simulation](#mocking-and-simulation)
- [Testing Circuit Breakers](#testing-circuit-breakers)
- [Testing Context Cancellation](#testing-context-cancellation)
- [Performance Testing](#performance-testing)
- [Common Patterns](#common-patterns)
- [Troubleshooting](#troubleshooting)

## Overview

Integration tests verify that RetryFS works correctly with real or realistic filesystem implementations. Unlike unit tests that use simple in-memory filesystems, integration tests should:

1. Test realistic failure scenarios
2. Verify retry behavior with actual I/O operations
3. Validate observability features (metrics, logging)
4. Ensure thread-safety in concurrent scenarios
5. Test circuit breaker behavior under real load

## Testing Strategies

### 1. Test Pyramid Approach

```
┌─────────────────┐
│  E2E Tests      │  ← Few tests with real filesystems (S3, WebDAV, etc.)
├─────────────────┤
│  Integration    │  ← Moderate number with realistic simulators
│  Tests          │
├─────────────────┤
│  Unit Tests     │  ← Many tests with memfs
└─────────────────┘
```

### 2. What to Test at Each Level

**Unit Tests (memfs)**
- Core retry logic
- Backoff calculations
- Error classification
- Metrics recording
- Circuit breaker state transitions

**Integration Tests (Simulated failures)**
- Transient failure recovery
- Circuit breaker behavior under load
- Context cancellation
- Concurrent access patterns
- Metrics accuracy

**E2E Tests (Real filesystems)**
- Actual network errors (S3, WebDAV)
- Real-world failure patterns
- Performance characteristics
- Production configuration validation

## Testing with Real Filesystems

### Example: Testing with S3

```go
func TestRetryFS_S3Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping S3 integration test")
    }

    // Setup S3 client
    sess := session.Must(session.NewSession(&aws.Config{
        Region: aws.String("us-east-1"),
    }))

    s3fs := s3billy.New(sess, "test-bucket")

    // Wrap with RetryFS
    fs := retryfs.New(s3fs,
        retryfs.WithPolicy(&retryfs.Policy{
            MaxAttempts: 5,
            BaseDelay:   100 * time.Millisecond,
            MaxDelay:    5 * time.Second,
            Jitter:      0.2,
            Multiplier:  2.0,
        }),
        retryfs.WithCircuitBreaker(retryfs.NewCircuitBreaker()),
    ).(*retryfs.RetryFS)

    // Test operations
    t.Run("CreateAndRead", func(t *testing.T) {
        testFile := "/integration-test-" + uuid.New().String()

        // Create file
        f, err := fs.Create(testFile)
        if err != nil {
            t.Fatalf("Failed to create file: %v", err)
        }

        content := []byte("test content")
        if _, err := f.Write(content); err != nil {
            t.Fatalf("Failed to write: %v", err)
        }
        f.Close()

        // Read file
        f2, err := fs.Open(testFile)
        if err != nil {
            t.Fatalf("Failed to open file: %v", err)
        }
        defer f2.Close()

        readContent, err := io.ReadAll(f2)
        if err != nil {
            t.Fatalf("Failed to read: %v", err)
        }

        if !bytes.Equal(readContent, content) {
            t.Errorf("Content mismatch: got %q, want %q", readContent, content)
        }

        // Cleanup
        if err := fs.Remove(testFile); err != nil {
            t.Logf("Cleanup failed: %v", err)
        }
    })

    // Verify metrics
    metrics := fs.GetMetrics()
    t.Logf("Metrics: %d attempts, %d retries, %d successes, %d failures",
        metrics.TotalAttempts, metrics.TotalRetries,
        metrics.TotalSuccesses, metrics.TotalFailures)
}
```

### Example: Testing with WebDAV

```go
func TestRetryFS_WebDAVIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping WebDAV integration test")
    }

    webdavURL := os.Getenv("WEBDAV_URL")
    if webdavURL == "" {
        t.Skip("WEBDAV_URL not set")
    }

    client := webdav.NewClient(webdavURL, http.DefaultClient)
    webdavFS := webdavbilly.New(client)

    // Configure for network operations
    fs := retryfs.New(webdavFS,
        retryfs.WithPolicy(&retryfs.Policy{
            MaxAttempts: 5,
            BaseDelay:   200 * time.Millisecond,
            MaxDelay:    10 * time.Second,
            Jitter:      0.3,
            Multiplier:  2.5,
        }),
        retryfs.WithErrorClassifier(func(err error) retryfs.ErrorClass {
            // Classify HTTP errors
            if strings.Contains(err.Error(), "503") {
                return retryfs.ErrorRetryable
            }
            if strings.Contains(err.Error(), "404") {
                return retryfs.ErrorPermanent
            }
            return retryfs.ErrorUnknown
        }),
    ).(*retryfs.RetryFS)

    // Test directory operations
    testDir := "/integration-test-" + time.Now().Format("20060102-150405")

    if err := fs.MkdirAll(testDir, 0755); err != nil {
        t.Fatalf("Failed to create directory: %v", err)
    }
    defer fs.Remove(testDir)

    // Create multiple files
    for i := 0; i < 10; i++ {
        fileName := fmt.Sprintf("%s/file%d.txt", testDir, i)
        f, err := fs.Create(fileName)
        if err != nil {
            t.Errorf("Failed to create %s: %v", fileName, err)
            continue
        }
        fmt.Fprintf(f, "Content %d", i)
        f.Close()
    }

    // List directory
    files, err := fs.ReadDir(testDir)
    if err != nil {
        t.Fatalf("Failed to read directory: %v", err)
    }

    if len(files) != 10 {
        t.Errorf("Expected 10 files, got %d", len(files))
    }
}
```

## Mocking and Simulation

### Creating a Failure-Injecting Filesystem

```go
type FailureInjector struct {
    underlying billy.Filesystem
    mu         sync.Mutex

    // Failure configuration
    failureRate    float64
    failurePattern string // "random", "burst", "gradual"
    opsUntilFail   int
    opCount        int

    // Metrics
    injectedFailures int
}

func NewFailureInjector(fs billy.Filesystem, config FailureConfig) *FailureInjector {
    return &FailureInjector{
        underlying:     fs,
        failureRate:    config.Rate,
        failurePattern: config.Pattern,
        opsUntilFail:   config.OpsUntilFailure,
    }
}

func (fi *FailureInjector) shouldFail() bool {
    fi.mu.Lock()
    defer fi.mu.Unlock()

    fi.opCount++

    switch fi.failurePattern {
    case "random":
        return rand.Float64() < fi.failureRate
    case "burst":
        // Fail in bursts
        if fi.opCount%100 < 10 {
            return rand.Float64() < 0.8 // High failure rate in burst
        }
        return false
    case "gradual":
        // Gradually increasing failure rate
        currentRate := fi.failureRate * float64(fi.opCount) / 1000.0
        return rand.Float64() < currentRate
    default:
        return false
    }
}

func (fi *FailureInjector) Open(filename string) (billy.File, error) {
    if fi.shouldFail() {
        fi.mu.Lock()
        fi.injectedFailures++
        fi.mu.Unlock()
        return nil, errors.New("injected failure: temporary error")
    }
    return fi.underlying.Open(filename)
}

// Example usage
func TestWithFailureInjection(t *testing.T) {
    underlying := memfs.New()
    injector := NewFailureInjector(underlying, FailureConfig{
        Rate:    0.2,
        Pattern: "random",
    })

    fs := retryfs.New(injector,
        retryfs.WithPolicy(&retryfs.Policy{
            MaxAttempts: 5,
            BaseDelay:   10 * time.Millisecond,
            MaxDelay:    100 * time.Millisecond,
            Jitter:      0.1,
            Multiplier:  2.0,
        }),
    ).(*retryfs.RetryFS)

    // Run operations
    successCount := 0
    for i := 0; i < 100; i++ {
        if err := fs.MkdirAll(fmt.Sprintf("/dir%d", i), 0755); err == nil {
            successCount++
        }
    }

    t.Logf("Success rate: %.2f%% with %d injected failures",
        float64(successCount)/100.0*100, injector.injectedFailures)
}
```

### Simulating Network Latency

```go
type LatencySimulator struct {
    underlying billy.Filesystem
    minLatency time.Duration
    maxLatency time.Duration
}

func (ls *LatencySimulator) addLatency() {
    delay := ls.minLatency + time.Duration(rand.Int63n(int64(ls.maxLatency-ls.minLatency)))
    time.Sleep(delay)
}

func (ls *LatencySimulator) Stat(filename string) (fs.FileInfo, error) {
    ls.addLatency()
    return ls.underlying.Stat(filename)
}

// Example: Test with realistic latency
func TestWithNetworkLatency(t *testing.T) {
    underlying := memfs.New()
    simulator := &LatencySimulator{
        underlying: underlying,
        minLatency: 50 * time.Millisecond,
        maxLatency: 200 * time.Millisecond,
    }

    fs := retryfs.New(simulator).(*retryfs.RetryFS)

    start := time.Now()
    _, err := fs.Stat("/nonexistent")
    duration := time.Since(start)

    t.Logf("Operation took %s (expected 50-200ms)", duration)

    if err == nil {
        t.Error("Expected error for nonexistent file")
    }
}
```

## Testing Circuit Breakers

### Testing Circuit Breaker Opens

```go
func TestCircuitBreakerOpens(t *testing.T) {
    underlying := newAlwaysFailingFS()

    var stateChanges []string
    cb := retryfs.NewCircuitBreaker()
    cb.FailureThreshold = 5
    cb.OnStateChange = func(from, to retryfs.State) {
        stateChanges = append(stateChanges,
            fmt.Sprintf("%s->%s", from, to))
    }

    fs := retryfs.New(underlying,
        retryfs.WithCircuitBreaker(cb),
        retryfs.WithPolicy(&retryfs.Policy{
            MaxAttempts: 1,
            BaseDelay:   1 * time.Millisecond,
        }),
    ).(*retryfs.RetryFS)

    // Trigger failures
    for i := 0; i < 10; i++ {
        _ = fs.MkdirAll(fmt.Sprintf("/dir%d", i), 0755)

        if cb.GetState() == retryfs.StateOpen {
            t.Logf("Circuit opened after %d operations", i+1)
            break
        }
    }

    // Wait for async callback
    time.Sleep(100 * time.Millisecond)

    if len(stateChanges) == 0 {
        t.Error("Expected circuit breaker to change state")
    }

    t.Logf("State changes: %v", stateChanges)
}
```

### Testing Circuit Breaker Recovery

```go
func TestCircuitBreakerRecovery(t *testing.T) {
    failingFS := newFailingFS(memfs.New(), 10) // Fail 10 times then succeed

    cb := retryfs.NewCircuitBreaker()
    cb.FailureThreshold = 3
    cb.SuccessThreshold = 2
    cb.Timeout = 100 * time.Millisecond

    fs := retryfs.New(failingFS,
        retryfs.WithCircuitBreaker(cb),
        retryfs.WithPolicy(&retryfs.Policy{
            MaxAttempts: 1,
            BaseDelay:   1 * time.Millisecond,
        }),
    ).(*retryfs.RetryFS)

    // Open the circuit
    for i := 0; i < 5; i++ {
        _ = fs.MkdirAll("/test", 0755)
    }

    if cb.GetState() != retryfs.StateOpen {
        t.Error("Expected circuit to be open")
    }

    // Wait for timeout
    time.Sleep(150 * time.Millisecond)

    // Next call should succeed (half-open -> closed)
    if err := fs.MkdirAll("/test", 0755); err != nil {
        t.Errorf("Expected success in half-open state: %v", err)
    }

    // One more success should close the circuit
    if err := fs.MkdirAll("/test2", 0755); err != nil {
        t.Errorf("Expected success: %v", err)
    }

    time.Sleep(100 * time.Millisecond)

    if cb.GetState() != retryfs.StateClosed {
        t.Errorf("Expected circuit to be closed, got %s", cb.GetState())
    }
}
```

## Testing Context Cancellation

### Testing Deadline Exceeded

```go
func TestContextDeadline(t *testing.T) {
    underlying := &SlowFilesystem{
        underlying: memfs.New(),
        delay:      500 * time.Millisecond,
    }

    fs := retryfs.New(underlying).(*retryfs.RetryFS)

    ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()

    start := time.Now()
    err := fs.MkdirAllContext(ctx, "/test", 0755)
    duration := time.Since(start)

    if err != context.DeadlineExceeded {
        t.Errorf("Expected DeadlineExceeded, got %v", err)
    }

    if duration > 200*time.Millisecond {
        t.Errorf("Operation should have been cancelled quickly, took %s", duration)
    }
}
```

### Testing Cancellation During Retry

```go
func TestContextCancellationDuringRetry(t *testing.T) {
    failingFS := newFailingFS(memfs.New(), 100) // Always fail

    fs := retryfs.New(failingFS,
        retryfs.WithPolicy(&retryfs.Policy{
            MaxAttempts: 10,
            BaseDelay:   100 * time.Millisecond,
            MaxDelay:    1 * time.Second,
            Jitter:      0,
            Multiplier:  2.0,
        }),
    ).(*retryfs.RetryFS)

    ctx, cancel := context.WithCancel(context.Background())

    // Cancel after 250ms (should be during backoff)
    go func() {
        time.Sleep(250 * time.Millisecond)
        cancel()
    }()

    start := time.Now()
    err := fs.MkdirAllContext(ctx, "/test", 0755)
    duration := time.Since(start)

    if err != context.Canceled {
        t.Errorf("Expected Canceled, got %v", err)
    }

    // Should cancel quickly, not wait for all retries
    if duration > 500*time.Millisecond {
        t.Errorf("Cancellation took too long: %s", duration)
    }

    t.Logf("Cancelled after %s", duration)
}
```

## Performance Testing

### Benchmarking with Real Filesystems

```go
func BenchmarkRealFilesystem(b *testing.B) {
    // Use actual filesystem
    tmpDir, err := os.MkdirTemp("", "retryfs-bench-*")
    if err != nil {
        b.Fatal(err)
    }
    defer os.RemoveAll(tmpDir)

    osfs := osfs.New(tmpDir)
    fs := retryfs.New(osfs).(*retryfs.RetryFS)

    b.ResetTimer()
    b.ReportAllocs()

    for i := 0; i < b.N; i++ {
        fileName := fmt.Sprintf("file%d.txt", i)
        f, err := fs.Create(fileName)
        if err != nil {
            b.Fatal(err)
        }
        f.Write([]byte("test"))
        f.Close()
    }
}
```

### Load Testing Framework

```go
type LoadTestResult struct {
    TotalOps       int64
    SuccessfulOps  int64
    FailedOps      int64
    Duration       time.Duration
    OpsPerSecond   float64
}

func RunLoadTest(fs billy.Filesystem, config LoadTestConfig) *LoadTestResult {
    var totalOps, successOps, failedOps int64
    start := time.Now()

    var wg sync.WaitGroup
    for i := 0; i < config.Concurrency; i++ {
        wg.Add(1)
        go func(workerID int) {
            defer wg.Done()

            for j := 0; j < config.OpsPerWorker; j++ {
                atomic.AddInt64(&totalOps, 1)

                err := fs.MkdirAll(fmt.Sprintf("/worker%d/dir%d", workerID, j), 0755)
                if err == nil {
                    atomic.AddInt64(&successOps, 1)
                } else {
                    atomic.AddInt64(&failedOps, 1)
                }
            }
        }(i)
    }

    wg.Wait()
    duration := time.Since(start)

    return &LoadTestResult{
        TotalOps:      totalOps,
        SuccessfulOps: successOps,
        FailedOps:     failedOps,
        Duration:      duration,
        OpsPerSecond:  float64(totalOps) / duration.Seconds(),
    }
}
```

## Common Patterns

### Pattern 1: Test Setup with Custom Error Classifier

```go
func setupTestFS(t *testing.T, underlying billy.Filesystem) *retryfs.RetryFS {
    t.Helper()

    return retryfs.New(underlying,
        retryfs.WithPolicy(&retryfs.Policy{
            MaxAttempts: 3,
            BaseDelay:   10 * time.Millisecond,
            MaxDelay:    100 * time.Millisecond,
            Jitter:      0.1,
            Multiplier:  2.0,
        }),
        retryfs.WithErrorClassifier(func(err error) retryfs.ErrorClass {
            // Custom classification for your filesystem
            switch {
            case errors.Is(err, fs.ErrNotExist):
                return retryfs.ErrorPermanent
            case errors.Is(err, fs.ErrPermission):
                return retryfs.ErrorPermanent
            case strings.Contains(err.Error(), "timeout"):
                return retryfs.ErrorRetryable
            default:
                return retryfs.ErrorUnknown
            }
        }),
    ).(*retryfs.RetryFS)
}
```

### Pattern 2: Metrics Validation

```go
func validateMetrics(t *testing.T, fs *retryfs.RetryFS, expectedOps int64) {
    t.Helper()

    metrics := fs.GetMetrics()

    if metrics.TotalAttempts < expectedOps {
        t.Errorf("Expected at least %d attempts, got %d",
            expectedOps, metrics.TotalAttempts)
    }

    totalCompleted := metrics.TotalSuccesses + metrics.TotalFailures
    if totalCompleted != expectedOps {
        t.Errorf("Metrics mismatch: %d successes + %d failures != %d expected",
            metrics.TotalSuccesses, metrics.TotalFailures, expectedOps)
    }

    t.Logf("Metrics: %d attempts (%d retries), %d successes, %d failures",
        metrics.TotalAttempts, metrics.TotalRetries,
        metrics.TotalSuccesses, metrics.TotalFailures)
}
```

### Pattern 3: Cleanup Helper

```go
func withTempFilesystem(t *testing.T, fn func(fs billy.Filesystem)) {
    t.Helper()

    tmpDir, err := os.MkdirTemp("", "test-*")
    if err != nil {
        t.Fatal(err)
    }
    defer os.RemoveAll(tmpDir)

    osfs := osfs.New(tmpDir)
    fs := retryfs.New(osfs).(*retryfs.RetryFS)

    fn(fs)
}

// Usage
func TestSomething(t *testing.T) {
    withTempFilesystem(t, func(fs billy.Filesystem) {
        // Test code here
    })
}
```

## Troubleshooting

### Issue: Tests Timeout

**Symptoms:** Tests hang or take very long to complete.

**Solutions:**
1. Check retry policy - max attempts might be too high
2. Verify context timeouts are set appropriately
3. Ensure circuit breakers are configured correctly
4. Add timeout to tests: `go test -timeout 30s`

### Issue: Inconsistent Test Results

**Symptoms:** Tests pass sometimes, fail other times.

**Solutions:**
1. Add deterministic failure injection instead of random
2. Synchronize goroutines properly
3. Wait for async callbacks (circuit breaker state changes)
4. Use fixed random seeds for reproducibility

### Issue: Memory Leaks in Tests

**Symptoms:** Memory usage grows during tests.

**Solutions:**
1. Ensure all files are closed (`defer f.Close()`)
2. Check for goroutine leaks (circuit breaker callbacks)
3. Clean up temporary files/directories
4. Run with `-benchmem` to track allocations

### Issue: Circuit Breaker Not Tripping

**Symptoms:** Circuit breaker stays closed despite failures.

**Solutions:**
1. Lower FailureThreshold for tests
2. Ensure MaxAttempts is 1 (to fail fast)
3. Wait for async OnStateChange callback
4. Verify error classification (permanent errors don't count)

## Best Practices

1. **Use Table-Driven Tests**
   - Test multiple failure scenarios systematically
   - Easy to add new test cases

2. **Test in Isolation**
   - Each test should create its own filesystem
   - Avoid shared state between tests

3. **Use Helpers**
   - Create reusable setup/teardown helpers
   - Standardize test configuration

4. **Test Edge Cases**
   - Context cancellation during backoff
   - Circuit breaker state transitions
   - Concurrent access patterns

5. **Validate Observability**
   - Check metrics are accurate
   - Verify logging output
   - Test Prometheus integration

6. **Performance Baselines**
   - Benchmark with real filesystems
   - Establish acceptable overhead
   - Track performance over time

7. **Clean Up Resources**
   - Remove temporary files
   - Close all file handles
   - Cancel contexts

8. **Use Integration Test Tags**
   ```go
   // +build integration
   ```
   - Separate slow integration tests
   - Run with `go test -tags=integration`

## Example: Complete Integration Test Suite

```go
package mypackage_test

import (
    "context"
    "testing"
    "time"

    "github.com/absfs/retryfs"
    "github.com/go-git/go-billy/v5/memfs"
)

func TestIntegration_FullWorkflow(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }

    // Setup
    underlying := memfs.New()
    fs := retryfs.New(underlying,
        retryfs.WithPolicy(&retryfs.Policy{
            MaxAttempts: 5,
            BaseDelay:   10 * time.Millisecond,
            MaxDelay:    100 * time.Millisecond,
            Jitter:      0.1,
            Multiplier:  2.0,
        }),
        retryfs.WithCircuitBreaker(retryfs.NewCircuitBreaker()),
    ).(*retryfs.RetryFS)

    // Test 1: Create directory structure
    if err := fs.MkdirAll("/app/data/uploads", 0755); err != nil {
        t.Fatalf("Failed to create directories: %v", err)
    }

    // Test 2: Create and write file
    f, err := fs.Create("/app/data/config.json")
    if err != nil {
        t.Fatalf("Failed to create file: %v", err)
    }
    if _, err := f.Write([]byte(`{"version":"1.0"}`)); err != nil {
        t.Fatalf("Failed to write: %v", err)
    }
    f.Close()

    // Test 3: Read and verify
    data, err := fs.Open("/app/data/config.json")
    if err != nil {
        t.Fatalf("Failed to open file: %v", err)
    }
    defer data.Close()

    // Test 4: List directories
    files, err := fs.ReadDir("/app/data")
    if err != nil {
        t.Fatalf("Failed to read directory: %v", err)
    }
    if len(files) != 2 { // config.json and uploads/
        t.Errorf("Expected 2 entries, got %d", len(files))
    }

    // Test 5: Context with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancel()

    if err := fs.MkdirAllContext(ctx, "/app/logs", 0755); err != nil {
        t.Fatalf("Failed with context: %v", err)
    }

    // Validate metrics
    metrics := fs.GetMetrics()
    t.Logf("Final metrics: %+v", metrics)

    if metrics.TotalSuccesses == 0 {
        t.Error("Expected some successful operations")
    }
}
```

This guide provides comprehensive patterns for integration testing with RetryFS. Adapt these examples to your specific filesystem and requirements.
