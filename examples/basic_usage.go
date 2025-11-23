package main

import (
	"fmt"
	"log"

	"github.com/absfs/retryfs"
	"github.com/go-git/go-billy/v5/memfs"
)

func main() {
	// Create an underlying filesystem (in this example, we use memfs for demo)
	// In practice, you'd wrap network filesystems like S3, WebDAV, etc.
	underlying := memfs.New()

	// Wrap with retry logic using default settings
	fs := retryfs.New(underlying)

	// Use the filesystem normally - retries happen automatically
	err := fs.MkdirAll("/data/logs", 0755)
	if err != nil {
		log.Fatalf("Failed to create directory: %v", err)
	}

	// Create a file
	file, err := fs.Create("/data/logs/app.log")
	if err != nil {
		log.Fatalf("Failed to create file: %v", err)
	}
	defer file.Close()

	// Write to the file
	_, err = file.Write([]byte("Application started\n"))
	if err != nil {
		log.Fatalf("Failed to write: %v", err)
	}

	fmt.Println("Successfully created and wrote to file with automatic retries!")

	// Access metrics to see retry statistics
	if rfs, ok := fs.(*retryfs.RetryFS); ok {
		metrics := rfs.GetMetrics()
		fmt.Printf("Total attempts: %d\n", metrics.TotalAttempts)
		fmt.Printf("Total retries: %d\n", metrics.TotalRetries)
		fmt.Printf("Total successes: %d\n", metrics.TotalSuccesses)
		fmt.Printf("Total failures: %d\n", metrics.TotalFailures)
	}
}
