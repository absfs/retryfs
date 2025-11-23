package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/absfs/retryfs"
	"github.com/go-git/go-billy/v5/memfs"
)

// This example shows a complete production-ready setup
func main() {
	// In production, replace memfs with your actual filesystem
	// For example: s3fs, webdavfs, sftpfs, etc.
	underlying := memfs.New()

	// 1. Configure retry policy for network operations
	policy := &retryfs.Policy{
		MaxAttempts: 5,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    30 * time.Second,
		Jitter:      0.25, // ±25% randomness to prevent thundering herd
		Multiplier:  2.0,
	}

	// 2. Set up circuit breaker to prevent cascading failures
	cb := &retryfs.CircuitBreaker{
		FailureThreshold: 10,
		SuccessThreshold: 3,
		Timeout:          60 * time.Second,
		OnStateChange: func(from, to retryfs.State) {
			log.Printf("[CircuitBreaker] State transition: %s -> %s", from, to)
		},
	}

	// 3. Configure custom error classification if needed
	customClassifier := func(err error) retryfs.ErrorClass {
		// Example: Classify application-specific errors
		if errors.Is(err, errQuotaExceeded) {
			return retryfs.ErrorPermanent
		}
		if errors.Is(err, errRateLimited) {
			return retryfs.ErrorRetryable
		}
		// Fall back to default classification
		return retryfs.DefaultConfig().ErrorClassifier(err)
	}

	// 4. Set up retry callback for observability
	onRetry := func(op retryfs.Operation, attempt int, err error) {
		log.Printf("[Retry] Operation=%s Attempt=%d Error=%v", op, attempt, err)
	}

	// 5. Create the retry-wrapped filesystem
	fs := retryfs.New(
		underlying,
		retryfs.WithPolicy(policy),
		retryfs.WithCircuitBreaker(cb),
		retryfs.WithErrorClassifier(customClassifier),
		retryfs.WithOnRetry(onRetry),
	)

	// Cast to access extended features
	rfs := fs.(*retryfs.RetryFS)

	// 6. Use the filesystem with context for deadline control
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create directory structure
	if err := rfs.MkdirAllContext(ctx, "/app/data", 0755); err != nil {
		log.Fatalf("Failed to create directory: %v", err)
	}

	// Write application data
	file, err := rfs.CreateContext(ctx, "/app/data/config.json")
	if err != nil {
		log.Fatalf("Failed to create file: %v", err)
	}
	defer file.Close()

	config := []byte(`{"version": "1.0", "debug": false}`)
	if _, err := file.Write(config); err != nil {
		log.Fatalf("Failed to write config: %v", err)
	}

	fmt.Println("Application data written successfully!")

	// 7. Monitor metrics
	metrics := rfs.GetMetrics()
	fmt.Printf("\n=== Metrics ===\n")
	fmt.Printf("Total Attempts:  %d\n", metrics.TotalAttempts)
	fmt.Printf("Total Retries:   %d\n", metrics.TotalRetries)
	fmt.Printf("Total Successes: %d\n", metrics.TotalSuccesses)
	fmt.Printf("Total Failures:  %d\n", metrics.TotalFailures)

	fmt.Printf("\nErrors by Class:\n")
	for class, count := range metrics.ErrorsByClass {
		fmt.Printf("  %s: %d\n", class, count)
	}

	fmt.Printf("\nPer-Operation Metrics:\n")
	for op, opMetrics := range metrics.OperationMetrics {
		if opMetrics.Attempts > 0 {
			successRate := float64(opMetrics.Successes) / float64(opMetrics.Attempts) * 100
			fmt.Printf("  %s: %d attempts, %d retries, %.1f%% success\n",
				op, opMetrics.Attempts, opMetrics.Retries, successRate)
		}
	}

	// 8. Check circuit breaker status
	cbStats := cb.GetStats()
	fmt.Printf("\n=== Circuit Breaker ===\n")
	fmt.Printf("State: %s\n", cbStats.State)
	fmt.Printf("Consecutive Errors: %d\n", cbStats.ConsecutiveErrors)
	fmt.Printf("Consecutive Successes: %d\n", cbStats.ConsecutiveSuccess)

	fmt.Println("\n✓ All operations completed successfully with retryfs!")
}

// Example custom errors
var (
	errQuotaExceeded = errors.New("storage quota exceeded")
	errRateLimited   = errors.New("rate limit exceeded")
)
