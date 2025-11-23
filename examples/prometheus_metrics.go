package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/absfs/retryfs"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// Create underlying filesystem
	underlying := memfs.New()

	// Configure retry policy
	policy := &retryfs.Policy{
		MaxAttempts: 5,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    10 * time.Second,
		Jitter:      0.25,
		Multiplier:  2.0,
	}

	// Create circuit breaker
	cb := retryfs.NewCircuitBreaker()
	cb.FailureThreshold = 5
	cb.SuccessThreshold = 2
	cb.Timeout = 30 * time.Second

	// Create RetryFS with circuit breaker
	fs := retryfs.New(
		underlying,
		retryfs.WithPolicy(policy),
		retryfs.WithCircuitBreaker(cb),
	).(*retryfs.RetryFS)

	// Create Prometheus registry
	registry := prometheus.NewRegistry()

	// Create and register Prometheus collector
	collector := retryfs.NewPrometheusCollector(fs, "myapp", "storage")
	err := collector.Register(registry)
	if err != nil {
		log.Fatalf("Failed to register Prometheus collector: %v", err)
	}

	// Create HTTP handler for metrics
	http.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	}))

	// Start metrics server in background
	go func() {
		fmt.Println("Starting Prometheus metrics server on :9090")
		fmt.Println("Visit http://localhost:9090/metrics to see metrics")
		if err := http.ListenAndServe(":9090", nil); err != nil {
			log.Printf("Metrics server error: %v", err)
		}
	}()

	// Give the server time to start
	time.Sleep(100 * time.Millisecond)

	// Perform some filesystem operations to generate metrics
	fmt.Println("\nPerforming filesystem operations...")

	// Create directories
	for i := 0; i < 5; i++ {
		path := fmt.Sprintf("/data/dir%d", i)
		if err := fs.MkdirAll(path, 0755); err != nil {
			log.Printf("Failed to create directory %s: %v", path, err)
		} else {
			fmt.Printf("Created directory: %s\n", path)
		}
	}

	// Create files
	for i := 0; i < 10; i++ {
		filename := fmt.Sprintf("/data/file%d.txt", i)
		file, err := fs.Create(filename)
		if err != nil {
			log.Printf("Failed to create file %s: %v", filename, err)
			continue
		}
		file.Close()
		fmt.Printf("Created file: %s\n", filename)
	}

	// Stat some files
	for i := 0; i < 5; i++ {
		filename := fmt.Sprintf("/data/file%d.txt", i)
		if _, err := fs.Stat(filename); err != nil {
			log.Printf("Failed to stat file %s: %v", filename, err)
		}
	}

	// Read directory
	if files, err := fs.ReadDir("/data"); err != nil {
		log.Printf("Failed to read directory: %v", err)
	} else {
		fmt.Printf("\nFound %d items in /data\n", len(files))
	}

	// Print metrics summary
	metrics := fs.GetMetrics()
	fmt.Printf("\n=== Metrics Summary ===\n")
	fmt.Printf("Total Attempts:  %d\n", metrics.TotalAttempts)
	fmt.Printf("Total Retries:   %d\n", metrics.TotalRetries)
	fmt.Printf("Total Successes: %d\n", metrics.TotalSuccesses)
	fmt.Printf("Total Failures:  %d\n", metrics.TotalFailures)

	if cb != nil {
		stats := cb.GetStats()
		fmt.Printf("\n=== Circuit Breaker ===\n")
		fmt.Printf("State: %s\n", stats.State)
		fmt.Printf("Consecutive Errors: %d\n", stats.ConsecutiveErrors)
		fmt.Printf("Consecutive Successes: %d\n", stats.ConsecutiveSuccess)
	}

	fmt.Println("\n=== Prometheus Metrics ===")
	fmt.Println("Metrics are available at: http://localhost:9090/metrics")
	fmt.Println("\nExample queries:")
	fmt.Println("  - myapp_storage_attempts_total")
	fmt.Println("  - myapp_storage_retries_total")
	fmt.Println("  - myapp_storage_successes_total")
	fmt.Println("  - myapp_storage_failures_total")
	fmt.Println("  - myapp_storage_errors_total")
	fmt.Println("  - myapp_storage_circuit_state")
	fmt.Println("\nPress Ctrl+C to exit...")

	// Keep server running
	select {}
}
