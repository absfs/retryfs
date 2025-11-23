package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/absfs/retryfs"
	"github.com/go-git/go-billy/v5/memfs"
)

func main() {
	underlying := memfs.New()

	// Configure with longer retry delays
	policy := &retryfs.Policy{
		MaxAttempts: 10,
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    10 * time.Second,
		Jitter:      0.1,
		Multiplier:  2.0,
	}

	fs := retryfs.New(underlying, retryfs.WithPolicy(policy))

	// Example 1: Operation with timeout
	fmt.Println("Example 1: Operation with 2 second timeout")
	ctx1, cancel1 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel1()

	start := time.Now()
	err := fs.(*retryfs.RetryFS).MkdirAllContext(ctx1, "/data/logs", 0755)
	duration := time.Since(start)

	if err != nil {
		log.Printf("Operation failed: %v (took %v)", err, duration)
	} else {
		log.Printf("Operation succeeded (took %v)", duration)
	}

	// Example 2: Manual cancellation
	fmt.Println("\nExample 2: Manual cancellation")
	ctx2, cancel2 := context.WithCancel(context.Background())

	// Cancel after 1 second
	go func() {
		time.Sleep(1 * time.Second)
		fmt.Println("Cancelling operation...")
		cancel2()
	}()

	start = time.Now()
	err = fs.(*retryfs.RetryFS).CreateContext(ctx2, "/data/file.txt")
	duration = time.Since(start)

	if err != nil {
		log.Printf("Operation cancelled: %v (took %v)", err, duration)
	} else {
		log.Printf("Operation succeeded before cancellation (took %v)", duration)
	}

	// Example 3: Using context for graceful shutdown
	fmt.Println("\nExample 3: Batch operations with shared deadline")
	ctx3, cancel3 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel3()

	files := []string{"data1.txt", "data2.txt", "data3.txt"}
	for _, filename := range files {
		file, err := fs.(*retryfs.RetryFS).CreateContext(ctx3, "/data/"+filename)
		if err != nil {
			log.Printf("Failed to create %s: %v", filename, err)
			continue
		}
		file.Close()
		fmt.Printf("Created %s\n", filename)
	}

	fmt.Println("\nAll operations completed!")
}
