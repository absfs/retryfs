# RetryFS Best Practices

This guide provides recommendations for using RetryFS effectively in production environments.

## Table of Contents

- [When to Use RetryFS](#when-to-use-retryfs)
- [Configuration Guidelines](#configuration-guidelines)
- [Error Classification](#error-classification)
- [Circuit Breaker Patterns](#circuit-breaker-patterns)
- [Monitoring and Observability](#monitoring-and-observability)
- [Performance Considerations](#performance-considerations)
- [Common Pitfalls](#common-pitfalls)
- [Production Deployment](#production-deployment)

---

## When to Use RetryFS

### ✅ Good Use Cases

**Network Filesystems**:
- S3-backed filesystems (`s3fs`)
- WebDAV remote storage
- SFTP/FTP connections
- Cloud storage APIs

**Characteristics**:
- High latency connections
- Intermittent connectivity issues
- Rate-limited APIs
- Temporary service unavailability

### ❌ Not Recommended For

**Local Filesystems**:
- In-memory filesystems (`memfs`)
- Local disk I/O
- tmpfs or ramdisk

**Reasons**:
- Adds unnecessary overhead
- Local operations are already reliable
- Retry logic won't fix hardware failures

---

## Configuration Guidelines

### Retry Policy Selection

#### Conservative (Default - Recommended for Writes)
```go
&retryfs.Policy{
    MaxAttempts: 3,
    BaseDelay:   100 * time.Millisecond,
    MaxDelay:    5 * time.Second,
    Jitter:      0.1,
    Multiplier:  2.0,
}
```
- **Use for**: Write operations, destructive operations
- **Why**: Minimizes risk of duplicate operations

#### Aggressive (Recommended for Reads)
```go
&retryfs.Policy{
    MaxAttempts: 10,
    BaseDelay:   50 * time.Millisecond,
    MaxDelay:    30 * time.Second,
    Jitter:      0.25,
    Multiplier:  2.0,
}
```
- **Use for**: Read operations, stat, list directories
- **Why**: Reads are idempotent and cheap to retry

#### Balanced
```go
&retryfs.Policy{
    MaxAttempts: 5,
    BaseDelay:   100 * time.Millisecond,
    MaxDelay:    10 * time.Second,
    Jitter:      0.2,
    Multiplier:  2.0,
}
```
- **Use for**: Mixed workloads, general purpose
- **Why**: Good compromise between reliability and latency

### Per-Operation Policies

Configure different retry strategies for different operation types:

```go
config := &retryfs.Config{
    DefaultPolicy: balancedPolicy,
    OperationPolicies: map[retryfs.Operation]*retryfs.Policy{
        // Aggressive for reads
        retryfs.OpOpen:    aggressivePolicy,
        retryfs.OpStat:    aggressivePolicy,
        retryfs.OpReadDir: aggressivePolicy,

        // Conservative for writes
        retryfs.OpCreate:   conservativePolicy,
        retryfs.OpRemove:   conservativePolicy,
        retryfs.OpRename:   conservativePolicy,
    },
}
```

---

## Error Classification

### Custom Error Classifier

Always implement a custom error classifier for your specific filesystem backend:

```go
func classifyS3Error(err error) retryfs.ErrorClass {
    if err == nil {
        return retryfs.ErrorUnknown
    }

    // Check for S3-specific errors
    var s3Err smithy.APIError
    if errors.As(err, &s3Err) {
        switch s3Err.ErrorCode() {
        // Retryable errors
        case "RequestTimeout", "ServiceUnavailable":
            return retryfs.ErrorRetryable
        case "SlowDown", "RequestLimitExceeded":
            return retryfs.ErrorRetryable

        // Permanent errors
        case "NoSuchBucket", "NoSuchKey":
            return retryfs.ErrorPermanent
        case "AccessDenied", "InvalidAccessKeyId":
            return retryfs.ErrorPermanent
        }
    }

    // Fall back to default classifier
    return retryfs.DefaultConfig().ErrorClassifier(err)
}

fs := retryfs.New(s3Filesystem,
    retryfs.WithErrorClassifier(classifyS3Error),
)
```

### Error Classification Guidelines

**Mark as Retryable**:
- Network timeouts
- Connection refused/reset
- Service unavailable (503)
- Rate limiting (429)
- Temporary resource exhaustion

**Mark as Permanent**:
- Not found (404)
- Permission denied (403)
- Invalid arguments (400)
- Quota exceeded
- Resource doesn't exist

**Unknown → Retry with Caution**:
- Default behavior: retry conservatively
- Monitor these errors closely
- Classify them explicitly once identified

---

## Circuit Breaker Patterns

### Global Circuit Breaker

Use for overall backend health:

```go
cb := retryfs.NewCircuitBreaker()
cb.FailureThreshold = 5   // Open after 5 failures
cb.SuccessThreshold = 2   // Close after 2 successes
cb.Timeout = 60 * time.Second

cb.OnStateChange = func(from, to retryfs.State) {
    log.Printf("Circuit breaker: %s -> %s", from, to)
    // Alert on-call engineer
    if to == retryfs.StateOpen {
        alerting.Notify("RetryFS circuit opened - backend may be down")
    }
}

fs := retryfs.New(backend, retryfs.WithCircuitBreaker(cb))
```

**When to use**:
- Single backend service
- Want to fail fast when entire service is down
- Simpler configuration

### Per-Operation Circuit Breakers

Use for granular control:

```go
pocb := retryfs.NewPerOperationCircuitBreaker(&retryfs.CircuitBreakerConfig{
    FailureThreshold: 5,
    SuccessThreshold: 2,
    Timeout:          30 * time.Second,
    OnStateChange: func(op retryfs.Operation, from, to retryfs.State) {
        log.Printf("[%s] Circuit: %s -> %s", op, from, to)
    },
})

fs := retryfs.New(backend,
    retryfs.WithPerOperationCircuitBreaker(pocb),
)
```

**When to use**:
- Want reads to continue even if writes fail
- Different operations have different failure characteristics
- Need fine-grained failure isolation

**Example scenario**: S3 ListObjects may work while PutObject is rate-limited

### Circuit Breaker Tuning

**Failure Threshold**:
- Too low (1-2): May open on transient blips
- Too high (>10): Takes too long to detect outages
- **Recommended**: 3-5 for most cases

**Success Threshold**:
- Too low (1): May close prematurely
- Too high (>5): Slow recovery
- **Recommended**: 2-3 for most cases

**Timeout**:
- Too short (<10s): Constant open/close cycling
- Too long (>5min): Slow recovery from outages
- **Recommended**: 30-60 seconds

---

## Monitoring and Observability

### Prometheus Metrics (Recommended)

```go
// Create Prometheus collector
collector := retryfs.NewPrometheusCollector(fs, "myapp", "storage")
prometheus.DefaultRegisterer.MustRegister(collector)

// Expose /metrics endpoint
http.Handle("/metrics", promhttp.Handler())
```

**Key Metrics to Monitor**:

1. **Retry Rate**: `rate(myapp_storage_retries_total[5m])`
   - Alert if > 10% of requests require retries

2. **Failure Rate**: `rate(myapp_storage_failures_total[5m])`
   - Alert if > 1% of requests fail after all retries

3. **Circuit Breaker State**: `myapp_storage_circuit_state`
   - Alert immediately if circuit opens (value = 1)

4. **Operation Latency**: `histogram_quantile(0.99, myapp_storage_operation_duration_seconds)`
   - Monitor p99 latency for degradation

### Logging

```go
fs := retryfs.New(backend,
    retryfs.WithOnRetry(func(op retryfs.Operation, attempt int, err error) {
        log.WithFields(log.Fields{
            "operation": op,
            "attempt":   attempt,
            "error":     err,
        }).Warn("Retrying operation")
    }),
)
```

### Alerting Rules

```yaml
# Prometheus alerting rules
groups:
  - name: retryfs
    rules:
      - alert: HighRetryRate
        expr: rate(myapp_storage_retries_total[5m]) > 0.1
        for: 5m
        annotations:
          summary: "High retry rate detected"

      - alert: CircuitBreakerOpen
        expr: myapp_storage_circuit_state == 1
        for: 1m
        annotations:
          summary: "RetryFS circuit breaker is open"

      - alert: HighFailureRate
        expr: rate(myapp_storage_failures_total[5m]) > 0.01
        for: 5m
        annotations:
          summary: "High failure rate after retries"
```

---

## Performance Considerations

### Overhead in Success Case

RetryFS adds minimal overhead when operations succeed:
- **~10-15% latency increase** for wrapper logic
- **Negligible CPU overhead** for backoff calculations
- **Minimal memory** per filesystem instance

### Retry Cost

Each retry adds:
- **Backoff delay**: Exponentially increasing wait time
- **Additional request**: Network round-trip + processing
- **Resource consumption**: Goroutines, memory, connections

**Example**: 3 retries with 100ms base delay = ~700ms added latency

### Optimization Strategies

#### 1. Tune Retry Delays

```go
// For low-latency networks (same region)
&retryfs.Policy{
    BaseDelay:  10 * time.Millisecond,
    MaxDelay:   1 * time.Second,
    Multiplier: 1.5,
}

// For high-latency networks (cross-region)
&retryfs.Policy{
    BaseDelay:  200 * time.Millisecond,
    MaxDelay:   30 * time.Second,
    Multiplier: 2.0,
}
```

#### 2. Use Per-Operation Policies

Give cheap operations more retries:

```go
OperationPolicies: map[retryfs.Operation]*retryfs.Policy{
    retryfs.OpStat: {MaxAttempts: 10},  // Cheap operation
    retryfs.OpCreate: {MaxAttempts: 3},  // Expensive operation
}
```

#### 3. Enable Circuit Breaker

Fail fast when backend is down instead of wasting retries:

```go
retryfs.WithCircuitBreaker(cb)
```

---

## Common Pitfalls

### ❌ Pitfall 1: Too Many Retries on Write Operations

**Problem**: Can cause duplicate writes or inconsistent state

**Solution**:
```go
OperationPolicies: map[retryfs.Operation]*retryfs.Policy{
    retryfs.OpCreate: {MaxAttempts: 3},  // Conservative
    retryfs.OpRemove: {MaxAttempts: 3},
    retryfs.OpRename: {MaxAttempts: 3},
}
```

### ❌ Pitfall 2: Not Using Jitter

**Problem**: Thundering herd when many clients retry simultaneously

**Solution**:
```go
Policy{
    Jitter: 0.25,  // ±25% randomness
}
```

### ❌ Pitfall 3: Ignoring Circuit Breaker State

**Problem**: Circuit opens but application doesn't handle it gracefully

**Solution**:
```go
err := fs.Open("/file.txt")
if errors.Is(err, retryfs.ErrCircuitOpen) {
    // Handle circuit open gracefully
    return cachedVersion, nil
}
```

### ❌ Pitfall 4: Not Monitoring Retry Metrics

**Problem**: Silent degradation, no visibility into issues

**Solution**: Always enable Prometheus metrics and set up alerts

### ❌ Pitfall 5: Using RetryFS for Local Filesystems

**Problem**: Unnecessary overhead, won't help with disk failures

**Solution**: Only wrap network filesystems

---

## Production Deployment

### Deployment Checklist

- [ ] Configured custom error classifier for your backend
- [ ] Set per-operation retry policies
- [ ] Enabled circuit breaker with appropriate thresholds
- [ ] Integrated Prometheus metrics
- [ ] Set up alerting rules
- [ ] Tested retry behavior with failure injection
- [ ] Documented expected retry rates
- [ ] Load tested with retryfs enabled

### Configuration Template

```go
// Production-ready configuration
func NewProductionRetryFS(backend absfs.FileSystem) absfs.FileSystem {
    // Configure retry policy
    policy := &retryfs.Policy{
        MaxAttempts: 5,
        BaseDelay:   100 * time.Millisecond,
        MaxDelay:    10 * time.Second,
        Jitter:      0.25,
        Multiplier:  2.0,
    }

    // Configure circuit breaker
    cb := retryfs.NewCircuitBreaker()
    cb.FailureThreshold = 5
    cb.SuccessThreshold = 2
    cb.Timeout = 60 * time.Second
    cb.OnStateChange = func(from, to retryfs.State) {
        log.Printf("Circuit breaker: %s -> %s", from, to)
        if to == retryfs.StateOpen {
            metrics.IncrementCircuitOpenCount()
            alerting.Notify("Storage circuit breaker opened")
        }
    }

    // Per-operation policies
    config := &retryfs.Config{
        DefaultPolicy: policy,
        OperationPolicies: map[retryfs.Operation]*retryfs.Policy{
            // Reads: more aggressive
            retryfs.OpOpen:    {MaxAttempts: 10, BaseDelay: 50 * time.Millisecond},
            retryfs.OpStat:    {MaxAttempts: 10, BaseDelay: 50 * time.Millisecond},
            retryfs.OpReadDir: {MaxAttempts: 10, BaseDelay: 50 * time.Millisecond},

            // Writes: more conservative
            retryfs.OpCreate: {MaxAttempts: 3, BaseDelay: 200 * time.Millisecond},
            retryfs.OpRemove: {MaxAttempts: 3, BaseDelay: 200 * time.Millisecond},
            retryfs.OpRename: {MaxAttempts: 3, BaseDelay: 200 * time.Millisecond},
        },
        ErrorClassifier: customErrorClassifier,
        OnRetry: func(op retryfs.Operation, attempt int, err error) {
            log.WithFields(log.Fields{
                "operation": op,
                "attempt":   attempt,
                "error":     err.Error(),
            }).Warn("Retrying filesystem operation")
            metrics.IncrementRetryCount(string(op))
        },
    }

    // Create RetryFS
    fs := retryfs.New(
        backend,
        retryfs.WithConfig(config),
        retryfs.WithCircuitBreaker(cb),
    ).(*retryfs.RetryFS)

    // Register Prometheus collector
    collector := retryfs.NewPrometheusCollector(fs, "myapp", "storage")
    prometheus.DefaultRegisterer.MustRegister(collector)

    return fs
}
```

### Health Checks

```go
func healthCheck(fs *retryfs.RetryFS) error {
    // Check circuit breaker state
    if cb := fs.GetCircuitBreaker(); cb != nil {
        if cb.GetState() == retryfs.StateOpen {
            return fmt.Errorf("circuit breaker is open")
        }
    }

    // Check failure rate
    metrics := fs.GetMetrics()
    if metrics.TotalAttempts > 100 {
        failureRate := float64(metrics.TotalFailures) / float64(metrics.TotalAttempts)
        if failureRate > 0.05 {  // 5% failure threshold
            return fmt.Errorf("high failure rate: %.2f%%", failureRate*100)
        }
    }

    return nil
}
```

---

## Summary

**Key Takeaways**:

1. **Only use RetryFS for network filesystems**
2. **Always implement custom error classifiers**
3. **Use different policies for reads vs writes**
4. **Enable circuit breakers to prevent retry storms**
5. **Monitor retry metrics with Prometheus**
6. **Set up alerting for high retry/failure rates**
7. **Test with failure injection before production**
8. **Add jitter to prevent thundering herd**

**For more examples, see**:
- `examples/complete_example.go` - Full production setup
- `examples/prometheus_metrics.go` - Metrics integration
- `examples/per_operation_circuit_breaker.go` - Per-operation patterns
