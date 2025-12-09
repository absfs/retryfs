package main

import (
	"fmt"
	"log"
	"time"

	"github.com/absfs/memfs"
	"github.com/absfs/retryfs"
)

func main() {
	underlying := memfs.New()

	// Configure per-operation circuit breakers
	// Different circuit breakers for different operation types
	pocb := retryfs.NewPerOperationCircuitBreaker(&retryfs.CircuitBreakerConfig{
		FailureThreshold: 3,  // Open after 3 failures
		SuccessThreshold: 2,  // Close after 2 successes
		Timeout:          30 * time.Second,
		OnStateChange: func(op retryfs.Operation, from, to retryfs.State) {
			fmt.Printf("[Circuit Breaker] %s: %s -> %s\n", op, from, to)
		},
	})

	// Configure retry policy
	policy := &retryfs.Policy{
		MaxAttempts: 5,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    5 * time.Second,
		Jitter:      0.25,
		Multiplier:  2.0,
	}

	// Create filesystem with per-operation circuit breakers
	fs := retryfs.New(
		underlying,
		retryfs.WithPolicy(policy),
		retryfs.WithPerOperationCircuitBreaker(pocb),
	).(*retryfs.RetryFS)

	fmt.Println("=== Per-Operation Circuit Breaker Demo ===\n")

	// Demonstrate independent circuit breakers
	fmt.Println("1. Normal operations (all circuit breakers closed):")

	// Create directory
	if err := fs.MkdirAll("/data", 0755); err != nil {
		log.Printf("Failed to create directory: %v", err)
	} else {
		fmt.Println("✓ Created directory /data")
	}

	// Create files
	for i := 0; i < 3; i++ {
		filename := fmt.Sprintf("/data/file%d.txt", i)
		file, err := fs.Create(filename)
		if err != nil {
			log.Printf("Failed to create %s: %v", filename, err)
			continue
		}
		file.Close()
		fmt.Printf("✓ Created file: %s\n", filename)
	}

	// Stat operations
	for i := 0; i < 3; i++ {
		filename := fmt.Sprintf("/data/file%d.txt", i)
		if _, err := fs.Stat(filename); err != nil {
			log.Printf("Failed to stat %s: %v", filename, err)
		}
	}

	// Show circuit breaker states
	fmt.Println("\n2. Circuit Breaker States:")
	states := pocb.GetAllStates()
	for op, state := range states {
		fmt.Printf("   %s: %s\n", op, state)
	}

	// Show detailed statistics
	fmt.Println("\n3. Circuit Breaker Statistics:")
	stats := pocb.GetAllStats()
	for op, stat := range stats {
		fmt.Printf("   %s:\n", op)
		fmt.Printf("      State: %s\n", stat.State)
		fmt.Printf("      Consecutive Errors: %d\n", stat.ConsecutiveErrors)
		fmt.Printf("      Consecutive Successes: %d\n", stat.ConsecutiveSuccess)
	}

	// Demonstrate independence: If writes fail, reads can still work
	fmt.Println("\n4. Independence Demonstration:")
	fmt.Println("   Even if one operation's circuit opens, others continue working")
	fmt.Println("   For example: Write failures don't affect read operations")

	// Show metrics
	metrics := fs.GetMetrics()
	fmt.Println("\n=== Overall Metrics ===")
	fmt.Printf("Total Attempts:  %d\n", metrics.TotalAttempts)
	fmt.Printf("Total Retries:   %d\n", metrics.TotalRetries)
	fmt.Printf("Total Successes: %d\n", metrics.TotalSuccesses)
	fmt.Printf("Total Failures:  %d\n", metrics.TotalFailures)

	fmt.Println("\nPer-Operation Metrics:")
	for op, opMetrics := range metrics.OperationMetrics {
		if opMetrics.Attempts > 0 {
			successRate := float64(opMetrics.Successes) / float64(opMetrics.Attempts) * 100
			fmt.Printf("  %s: %d attempts, %.1f%% success\n", op, opMetrics.Attempts, successRate)
		}
	}

	fmt.Println("\n✓ Demo completed successfully!")
	fmt.Println("\nKey Benefits of Per-Operation Circuit Breakers:")
	fmt.Println("  - Independent failure isolation per operation type")
	fmt.Println("  - Reads can continue even if writes are failing")
	fmt.Println("  - More granular control over failure handling")
	fmt.Println("  - Better resource utilization")
}
