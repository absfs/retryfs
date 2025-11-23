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

	// Create a circuit breaker to prevent retry storms
	cb := &retryfs.CircuitBreaker{
		FailureThreshold: 5,              // Open after 5 consecutive failures
		SuccessThreshold: 2,              // Close after 2 consecutive successes
		Timeout:          30 * time.Second, // Wait 30s before trying again
		OnStateChange: func(from, to retryfs.State) {
			log.Printf("Circuit breaker: %s -> %s", from, to)
		},
	}

	// Create filesystem with circuit breaker
	fs := retryfs.New(
		underlying,
		retryfs.WithCircuitBreaker(cb),
	)

	// Simulate some operations
	for i := 0; i < 10; i++ {
		err := fs.MkdirAll(fmt.Sprintf("/data/%d", i), 0755)
		if err != nil {
			if err == retryfs.ErrCircuitOpen {
				log.Printf("Circuit breaker is OPEN - failing fast!")
				break
			}
			log.Printf("Operation %d failed: %v", i, err)
		} else {
			log.Printf("Operation %d succeeded", i)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Check circuit breaker stats
	stats := cb.GetStats()
	fmt.Printf("\nCircuit Breaker Stats:\n")
	fmt.Printf("  State: %s\n", stats.State)
	fmt.Printf("  Consecutive Errors: %d\n", stats.ConsecutiveErrors)
	fmt.Printf("  Consecutive Successes: %d\n", stats.ConsecutiveSuccess)
}
