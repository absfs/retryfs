package main

import (
	"fmt"
	"log"
	"time"

	"github.com/absfs/retryfs"
	"github.com/go-git/go-billy/v5/memfs"
)

func main() {
	underlying := memfs.New()

	// Create a custom retry policy with aggressive settings
	customPolicy := &retryfs.Policy{
		MaxAttempts: 10,                     // Try up to 10 times
		BaseDelay:   50 * time.Millisecond,  // Start with 50ms delay
		MaxDelay:    5 * time.Second,        // Cap at 5 seconds
		Jitter:      0.2,                    // ±20% randomness
		Multiplier:  2.0,                    // Double delay each time
	}

	// Configure with custom policy and per-operation overrides
	config := &retryfs.Config{
		DefaultPolicy: customPolicy,
		OperationPolicies: map[retryfs.Operation]*retryfs.Policy{
			// More retries for open operations (they're cheap)
			retryfs.OpOpen: {
				MaxAttempts: 15,
				BaseDelay:   25 * time.Millisecond,
				MaxDelay:    3 * time.Second,
				Jitter:      0.25,
				Multiplier:  1.5,
			},
			// Fewer retries for write operations (be conservative)
			retryfs.OpCreate: {
				MaxAttempts: 5,
				BaseDelay:   100 * time.Millisecond,
				MaxDelay:    10 * time.Second,
				Jitter:      0.1,
				Multiplier:  2.5,
			},
		},
		ErrorClassifier: retryfs.DefaultConfig().ErrorClassifier,
	}

	// Add a callback to log retries
	onRetry := func(op retryfs.Operation, attempt int, err error) {
		log.Printf("Retry %d for operation %s: %v", attempt, op, err)
	}

	// Create filesystem with custom configuration
	fs := retryfs.New(
		underlying,
		retryfs.WithConfig(config),
		retryfs.WithOnRetry(onRetry),
	)

	// Use the filesystem
	err := fs.MkdirAll("/important/data", 0755)
	if err != nil {
		log.Fatalf("Failed: %v", err)
	}

	fmt.Println("Successfully used filesystem with custom retry policies!")
}
