# RetryFS Examples

This directory contains practical examples demonstrating how to use retryfs in various scenarios.

## Examples

### 1. Basic Usage (`basic_usage.go`)

The simplest way to use retryfs with default settings:

```bash
go run basic_usage.go
```

Shows:
- Creating a RetryFS with default configuration
- Using the filesystem normally (retries happen automatically)
- Accessing metrics to monitor retry behavior

### 2. Custom Policy (`custom_policy.go`)

Configuring custom retry policies:

```bash
go run custom_policy.go
```

Shows:
- Creating custom retry policies
- Per-operation policy configuration
- Retry callbacks for logging/monitoring

### 3. Circuit Breaker (`circuit_breaker.go`)

Using circuit breaker pattern to prevent retry storms:

```bash
go run circuit_breaker.go
```

Shows:
- Configuring a circuit breaker
- Monitoring circuit breaker state changes
- Failing fast when backend is down

### 4. Context & Deadlines (`context_deadline.go`)

Using context for deadline propagation and cancellation:

```bash
go run context_deadline.go
```

Shows:
- Operations with timeouts
- Manual cancellation
- Batch operations with shared deadlines

### 5. Complete Production Example (`complete_example.go`)

A comprehensive example showing all features together:

```bash
go run complete_example.go
```

Shows:
- Full production-ready configuration
- Circuit breaker + custom retry policies
- Custom error classification
- Context-aware operations
- Metrics monitoring
- Observability callbacks

## Running All Examples

To run all examples:

```bash
for example in *.go; do
  echo "=== Running $example ==="
  go run "$example"
  echo ""
done
```

## Adapting to Your Use Case

These examples use `memfs` (in-memory filesystem) for demonstration. In production, replace it with your actual filesystem implementation:

- **S3**: Use `github.com/absfs/s3fs`
- **WebDAV**: Use `github.com/absfs/webdavfs`
- **SFTP**: Use `github.com/absfs/sftpfs`
- **Any billy.Filesystem implementation**

Example:

```go
import "github.com/absfs/s3fs"

// Instead of memfs.New()
underlying := s3fs.New("my-bucket", s3fs.WithRegion("us-west-2"))

// Wrap with retry logic
fs := retryfs.New(underlying, retryfs.WithPolicy(myPolicy))
```

## Key Concepts Demonstrated

1. **Exponential Backoff**: Delays increase exponentially between retries
2. **Jitter**: Random variance prevents thundering herd
3. **Circuit Breaker**: Fail fast when backend is consistently down
4. **Error Classification**: Smart detection of retryable vs permanent errors
5. **Context Support**: Respect deadlines and cancellation
6. **Metrics**: Monitor retry behavior for observability
7. **Per-Operation Policies**: Different retry strategies for different operations
