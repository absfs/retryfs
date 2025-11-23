# retryfs

Automatic retry with exponential backoff for network filesystems in the AbsFS ecosystem.

## Overview

`retryfs` is a filesystem wrapper that provides automatic retry logic with configurable backoff strategies for unreliable or network-based filesystems. It implements the `billy.Filesystem` interface and transparently handles transient failures, making network filesystems more resilient without requiring application-level retry logic.

Key features:
- **Exponential backoff** with configurable parameters
- **Jitter** to prevent thundering herd problems
- **Circuit breaker** pattern to fail fast when backend is down
- **Error classification** to determine which errors are retryable
- **Configurable retry policies** per operation type
- **Metrics and observability** for monitoring retry behavior
- **Idempotency awareness** to safely retry operations

## Retry Strategies

### Exponential Backoff

The default retry strategy uses exponential backoff with jitter:

```
delay = min(maxDelay, baseDelay * 2^attempt) + random(-jitter, +jitter)
```

- **Base delay**: Initial delay before first retry (default: 100ms)
- **Max delay**: Maximum delay between retries (default: 30s)
- **Max attempts**: Maximum number of retry attempts (default: 5)
- **Jitter**: Random variance added to delay (default: ±25%)

### Circuit Breaker

Prevents cascading failures when the underlying filesystem is consistently unavailable:

- **Failure threshold**: Number of consecutive failures to open circuit (default: 5)
- **Success threshold**: Number of consecutive successes to close circuit (default: 2)
- **Timeout**: How long to wait in open state before trying again (default: 60s)
- **Half-open**: Test with single request before fully closing circuit

States:
- **Closed**: Normal operation, requests pass through
- **Open**: Too many failures, requests fail immediately
- **Half-Open**: Testing if backend has recovered

### Adaptive Backoff

Learn from success/failure patterns to optimize retry timing:

- Track success rates per operation type
- Adjust backoff parameters based on observed latency
- Detect patterns in transient vs permanent failures

## Implementation Phases

### Phase 1: Core Retry Logic

Foundation for retry behavior:

- Implement retry wrapper for billy.Filesystem interface
- Exponential backoff with jitter algorithm
- Configurable retry policies (per-operation and global)
- Error classification system (retryable vs non-retryable)
- Basic metrics collection (attempts, successes, failures)

### Phase 2: Error Classification

Intelligent error handling:

- Categorize errors by type (network, timeout, permission, not found, etc.)
- Default retry policies for common error classes
- Configurable error classifiers for custom error types
- Idempotency detection for write operations
- Support for wrapped/nested errors

### Phase 3: Circuit Breaker

Fail-fast behavior for degraded backends:

- Circuit breaker state machine
- Per-operation circuit breakers (optional)
- Health check probes during half-open state
- Configurable thresholds and timeouts
- Circuit breaker metrics and events

### Phase 4: Advanced Features

Enhanced reliability and observability:

- Adaptive backoff based on success patterns
- Request hedging (parallel retries after timeout)
- Priority queuing for retry attempts
- Deadline propagation and context support
- Structured logging and tracing integration
- Prometheus metrics exporter

### Phase 5: Testing and Documentation

Ensure reliability and ease of use:

- Comprehensive unit tests with mocked failures
- Integration tests with real network filesystems
- Chaos engineering tests (random failures)
- Performance benchmarks
- Usage examples and guides
- Best practices documentation

## API Design

### Basic Usage

```go
import (
    "github.com/absfs/retryfs"
    "github.com/absfs/s3fs"
)

// Wrap an unreliable filesystem with default retry policy
underlying := s3fs.New(bucket)
fs := retryfs.New(underlying)

// Use normally - retries are automatic
file, err := fs.Open("/data/file.txt")
```

### Custom Retry Policy

```go
policy := &retryfs.Policy{
    MaxAttempts:  3,
    BaseDelay:    50 * time.Millisecond,
    MaxDelay:     5 * time.Second,
    Jitter:       0.1, // ±10%
    Multiplier:   2.0, // Exponential factor
}

fs := retryfs.New(underlying, retryfs.WithPolicy(policy))
```

### Per-Operation Policies

```go
config := &retryfs.Config{
    DefaultPolicy: retryfs.DefaultPolicy,
    OperationPolicies: map[retryfs.Operation]*retryfs.Policy{
        retryfs.OpRead: {
            MaxAttempts: 5,
            BaseDelay:   100 * time.Millisecond,
        },
        retryfs.OpWrite: {
            MaxAttempts: 3,  // Fewer retries for writes
            BaseDelay:   200 * time.Millisecond,
        },
        retryfs.OpStat: {
            MaxAttempts: 10, // More retries for cheap operations
            BaseDelay:   50 * time.Millisecond,
        },
    },
}

fs := retryfs.New(underlying, retryfs.WithConfig(config))
```

### Circuit Breaker Configuration

```go
cb := &retryfs.CircuitBreaker{
    FailureThreshold: 10,
    SuccessThreshold: 2,
    Timeout:          60 * time.Second,
    OnStateChange: func(from, to retryfs.State) {
        log.Printf("Circuit breaker: %s -> %s", from, to)
    },
}

fs := retryfs.New(underlying, retryfs.WithCircuitBreaker(cb))
```

### Custom Error Classification

```go
classifier := func(err error) retryfs.ErrorClass {
    if errors.Is(err, s3.ErrThrottled) {
        return retryfs.ErrorRetryable
    }
    if errors.Is(err, s3.ErrNotFound) {
        return retryfs.ErrorPermanent
    }
    if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
        return retryfs.ErrorRetryable
    }
    return retryfs.ErrorUnknown
}

fs := retryfs.New(underlying, retryfs.WithErrorClassifier(classifier))
```

### Context and Deadlines

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

// Retries will respect context deadline
file, err := fs.OpenContext(ctx, "/data/file.txt")
```

## Usage Examples

### S3 Filesystem with Retries

```go
import (
    "github.com/absfs/retryfs"
    "github.com/absfs/s3fs"
)

// S3 can have transient failures due to rate limiting
s3 := s3fs.New("my-bucket", s3fs.WithRegion("us-west-2"))

// Wrap with retry logic
policy := &retryfs.Policy{
    MaxAttempts: 5,
    BaseDelay:   200 * time.Millisecond,
    MaxDelay:    10 * time.Second,
}

fs := retryfs.New(s3,
    retryfs.WithPolicy(policy),
    retryfs.WithCircuitBreaker(&retryfs.CircuitBreaker{
        FailureThreshold: 5,
        Timeout:          30 * time.Second,
    }),
)

// All operations automatically retry on transient failures
data, err := fs.ReadFile("/config.json")
```

### WebDAV Filesystem with Retries

```go
import (
    "github.com/absfs/retryfs"
    "github.com/absfs/webdavfs"
)

// WebDAV over flaky network connection
webdav := webdavfs.New("https://dav.example.com/files")

// Configure aggressive retries for network issues
fs := retryfs.New(webdav,
    retryfs.WithPolicy(&retryfs.Policy{
        MaxAttempts: 10,
        BaseDelay:   100 * time.Millisecond,
        MaxDelay:    30 * time.Second,
        Jitter:      0.25,
    }),
)

// Reliable file operations despite network issues
err := fs.MkdirAll("/backup/2024", 0755)
```

### Layered Reliability

```go
import (
    "github.com/absfs/retryfs"
    "github.com/absfs/cachefs"
    "github.com/absfs/s3fs"
)

// Combine caching and retries for maximum reliability
s3 := s3fs.New("my-bucket")
cached := cachefs.New(s3)
reliable := retryfs.New(cached)

// Cache reduces load, retries handle transient failures
fs := reliable
```

## Idempotency Considerations

Retry logic must be careful with non-idempotent operations to avoid unintended side effects.

### Idempotent Operations

Safe to retry without additional logic:
- **Read operations**: `Open`, `ReadFile`, `Stat`, `Lstat`, `Readlink`
- **Directory listings**: `ReadDir`, `Glob`
- **Idempotent writes**: `Create` (if it fails, file wasn't created), `Remove` (if it fails, file wasn't removed)

### Non-Idempotent Operations

Require careful handling:
- **Append operations**: Retrying may append data multiple times
- **Rename/Move**: Retrying after success but before confirmation may fail
- **Partial writes**: Write failures may leave partial data

### Strategies

1. **Track request IDs**: Use unique IDs to detect duplicate requests
2. **Check state before retry**: Verify operation didn't succeed before retrying
3. **Conservative policies**: Use fewer retries for risky operations
4. **Idempotency tokens**: Support backend-specific deduplication

Example implementation:

```go
// Before retrying a write, check if it already succeeded
func (r *RetryFS) retryWrite(path string, data []byte) error {
    var lastErr error
    for attempt := 0; attempt < r.policy.MaxAttempts; attempt++ {
        if attempt > 0 {
            // Check if previous attempt actually succeeded
            if info, err := r.fs.Stat(path); err == nil {
                if info.Size() == int64(len(data)) {
                    // Write probably succeeded on last attempt
                    return nil
                }
            }
            time.Sleep(r.backoff(attempt))
        }

        if err := r.fs.WriteFile(path, data, 0644); err != nil {
            lastErr = err
            continue
        }
        return nil
    }
    return lastErr
}
```

## Error Classification

Not all errors should trigger retries. `retryfs` classifies errors into categories:

### Retryable Errors

Transient failures that may succeed on retry:
- **Network errors**: Connection refused, timeout, temporary DNS failure
- **Rate limiting**: HTTP 429, S3 throttling
- **Temporary unavailability**: HTTP 503, backend overload
- **Concurrent access**: Optimistic locking failures
- **Resource exhaustion**: Temporary out of memory, file descriptors

### Non-Retryable Errors

Permanent failures that won't change on retry:
- **Not found**: File or directory doesn't exist (HTTP 404)
- **Permission denied**: Authentication or authorization failure (HTTP 403)
- **Invalid input**: Malformed path, invalid arguments (HTTP 400)
- **Not supported**: Operation not implemented
- **Quota exceeded**: Storage limit reached

### Unknown Errors

Errors that don't clearly fit a category:
- Default behavior: Retry with conservative policy
- Can be configured to treat as retryable or permanent
- Logged for manual classification

### Custom Classification

```go
// Define custom error types
var (
    ErrQuotaExceeded = errors.New("storage quota exceeded")
    ErrRateLimited   = errors.New("rate limited")
)

// Configure classifier
classifier := func(err error) retryfs.ErrorClass {
    switch {
    case errors.Is(err, ErrQuotaExceeded):
        return retryfs.ErrorPermanent
    case errors.Is(err, ErrRateLimited):
        return retryfs.ErrorRetryable
    default:
        return retryfs.ErrorUnknown
    }
}

fs := retryfs.New(underlying, retryfs.WithErrorClassifier(classifier))
```

## Performance Impact

Retry logic adds overhead that should be understood and monitored:

### Latency Impact

- **Success case**: Minimal overhead (function call + error check)
- **Retry case**: Additional latency = sum of backoff delays
- **Circuit open**: Near-zero overhead (fail immediately)

Example: 3 retries with 100ms, 200ms, 400ms delays = 700ms added latency

### Throughput Impact

- **No retries**: No impact on throughput
- **With retries**: Reduced effective throughput during failures
- **Circuit breaker**: Prevents wasted attempts on dead backend

### Resource Impact

- **Memory**: Minimal per-filesystem instance (config + circuit state)
- **Goroutines**: No additional goroutines in basic implementation
- **CPU**: Negligible for backoff calculations

### Optimization Strategies

1. **Tune retry parameters**: Balance reliability vs latency
2. **Per-operation policies**: Aggressive for cheap ops, conservative for expensive
3. **Circuit breaker**: Prevent retry storms
4. **Metrics**: Monitor retry rates to detect issues
5. **Adaptive backoff**: Learn optimal parameters from real traffic

### Benchmarks

```
BenchmarkOpen_NoRetry          100000    12500 ns/op
BenchmarkOpen_WithRetryWrapper 100000    12650 ns/op  (+1.2%)
BenchmarkOpen_WithRetry_1Fail  10000    125000 ns/op  (1 retry, 100ms delay)
BenchmarkOpen_WithRetry_3Fail   1000   1250000 ns/op  (3 retries, 700ms total)
```

## Monitoring and Observability

Track retry behavior to understand system reliability:

### Metrics

- `retryfs_attempts_total{operation, result}`: Total retry attempts
- `retryfs_retries_total{operation}`: Number of retries executed
- `retryfs_circuit_state{name}`: Current circuit breaker state (0=closed, 1=open, 2=half-open)
- `retryfs_errors_total{operation, class}`: Errors by classification
- `retryfs_operation_duration_seconds{operation}`: Operation latency histogram

### Logging

```go
logger := log.New(os.Stdout, "retryfs: ", log.LstdFlags)
fs := retryfs.New(underlying,
    retryfs.WithLogger(logger),
    retryfs.WithVerbosity(retryfs.LogRetries), // Log each retry attempt
)
```

### Events

```go
events := make(chan retryfs.Event, 100)
fs := retryfs.New(underlying, retryfs.WithEvents(events))

go func() {
    for event := range events {
        switch e := event.(type) {
        case *retryfs.RetryEvent:
            log.Printf("Retry %d/%d for %s: %v",
                e.Attempt, e.MaxAttempts, e.Operation, e.Error)
        case *retryfs.CircuitEvent:
            log.Printf("Circuit %s -> %s", e.From, e.To)
        }
    }
}()
```

## Documentation

- **[Best Practices Guide](BEST_PRACTICES.md)** - Production deployment guidelines, configuration recommendations, and common pitfalls
- **[Examples](examples/)** - Working examples demonstrating various features
- **API Reference** - See inline documentation and godoc

## Observability

RetryFS provides comprehensive observability features:

### Prometheus Metrics

```go
collector := retryfs.NewPrometheusCollector(fs, "myapp", "storage")
prometheus.DefaultRegisterer.MustRegister(collector)
```

**Available Metrics**:
- `retryfs_attempts_total{operation}` - Total operation attempts
- `retryfs_retries_total{operation}` - Retry count per operation
- `retryfs_successes_total{operation}` - Successful operations
- `retryfs_failures_total{operation}` - Failed operations (after all retries)
- `retryfs_errors_total{class}` - Errors by classification
- `retryfs_circuit_state` - Circuit breaker state (0=closed, 1=open, 2=half-open)
- `retryfs_circuit_consecutive_errors` - Consecutive error count
- `retryfs_circuit_consecutive_successes` - Consecutive success count

### Per-Operation Circuit Breakers

For fine-grained control, use per-operation circuit breakers:

```go
pocb := retryfs.NewPerOperationCircuitBreaker(&retryfs.CircuitBreakerConfig{
    FailureThreshold: 5,
    SuccessThreshold: 2,
    Timeout:          30 * time.Second,
    OnStateChange: func(op retryfs.Operation, from, to retryfs.State) {
        log.Printf("[%s] Circuit: %s -> %s", op, from, to)
    },
})

fs := retryfs.New(backend, retryfs.WithPerOperationCircuitBreaker(pocb))
```

This allows reads to continue working even if writes are failing.

## Contributing

Contributions are welcome! Please see the [AbsFS contribution guidelines](https://github.com/absfs/absfs).

## License

MIT License - see LICENSE file for details.
