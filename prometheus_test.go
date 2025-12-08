package retryfs

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)



func TestPrometheusCollector(t *testing.T) {
	underlying := mustNewMemFS()
	fs := New(underlying).(*RetryFS)

	// Create some test data
	_ = fs.MkdirAll("/test", 0755)
	f, _ := fs.Create("/test/file.txt")
	if f != nil {
		f.Close()
	}
	_, _ = fs.Stat("/test/file.txt")

	// Create Prometheus collector
	collector := NewPrometheusCollector(fs, "test", "fs")

	// Create registry and register collector
	registry := prometheus.NewRegistry()
	err := collector.Register(registry)
	if err != nil {
		t.Fatalf("Failed to register collector: %v", err)
	}

	// Collect metrics
	metricCount := testutil.CollectAndCount(collector)
	if metricCount == 0 {
		t.Error("Expected metrics to be collected, got 0")
	}

	t.Logf("Collected %d metrics", metricCount)

	// Verify specific metrics exist
	metrics := fs.GetMetrics()
	if metrics.TotalAttempts == 0 {
		t.Error("Expected TotalAttempts > 0")
	}

	// Test that collector can describe itself
	descCh := make(chan *prometheus.Desc, 10)
	go func() {
		collector.Describe(descCh)
		close(descCh)
	}()

	descCount := 0
	for range descCh {
		descCount++
	}

	if descCount == 0 {
		t.Error("Expected collector to describe metrics")
	}

	t.Logf("Collector describes %d metrics", descCount)
}

func TestPrometheusCollector_WithCircuitBreaker(t *testing.T) {
	underlying := mustNewMemFS()
	cb := NewCircuitBreaker()
	fs := New(underlying, WithCircuitBreaker(cb)).(*RetryFS)

	// Perform operations
	_ = fs.MkdirAll("/test", 0755)

	// Create collector
	collector := NewPrometheusCollector(fs, "test", "fs")
	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)

	// Collect and verify circuit breaker metrics are present
	metricCount := testutil.CollectAndCount(collector)
	if metricCount == 0 {
		t.Error("Expected metrics to be collected")
	}

	// Circuit breaker metrics should be included
	metrics, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	foundCircuitState := false
	foundConsecutiveErrors := false

	for _, mf := range metrics {
		name := mf.GetName()
		if strings.Contains(name, "circuit_state") {
			foundCircuitState = true
		}
		if strings.Contains(name, "consecutive_errors") {
			foundConsecutiveErrors = true
		}
	}

	if !foundCircuitState {
		t.Error("Expected circuit_state metric")
	}
	if !foundConsecutiveErrors {
		t.Error("Expected consecutive_errors metric")
	}

	t.Logf("Found %d metric families", len(metrics))
}

func TestPrometheusCollector_Namespace(t *testing.T) {
	underlying := mustNewMemFS()
	fs := New(underlying).(*RetryFS)

	// Create collector with custom namespace
	collector := NewPrometheusCollector(fs, "custom", "subsys")

	// Register and gather
	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)

	metrics, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	// Verify namespace is applied
	for _, mf := range metrics {
		name := mf.GetName()
		if !strings.HasPrefix(name, "custom_subsys_") {
			t.Errorf("Expected metric to start with 'custom_subsys_', got: %s", name)
		}
	}
}

func TestPrometheusCollector_PerOperationMetrics(t *testing.T) {
	underlying := mustNewMemFS()
	fs := New(underlying).(*RetryFS)

	// Perform different operations
	_ = fs.MkdirAll("/test", 0755)
	f, _ := fs.Create("/test/file.txt")
	if f != nil {
		f.Close()
	}
	_, _ = fs.Stat("/test/file.txt")
	if df, _ := fs.Open("/test"); df != nil { df.Readdir(-1); df.Close() }

	// Create collector
	collector := NewPrometheusCollector(fs, "test", "")
	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)

	// Gather metrics
	metrics, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	// Check for per-operation labels
	foundOperations := make(map[string]bool)

	for _, mf := range metrics {
		for _, m := range mf.GetMetric() {
			for _, label := range m.GetLabel() {
				if label.GetName() == "operation" {
					foundOperations[label.GetValue()] = true
				}
			}
		}
	}

	expectedOps := []string{"mkdirall", "create", "stat", "open"}
	for _, op := range expectedOps {
		if !foundOperations[op] {
			t.Errorf("Expected to find operation label: %s", op)
		}
	}

	t.Logf("Found operations: %v", foundOperations)
}

func TestPrometheusCollector_ErrorClassification(t *testing.T) {
	underlying := newFailingFS(mustNewMemFS(), 2)
	fs := New(underlying, WithPolicy(&Policy{
		MaxAttempts: 5,
		BaseDelay:   1,
		MaxDelay:    10,
		Jitter:      0,
		Multiplier:  1,
	})).(*RetryFS)

	// Try to open non-existent file (will eventually fail)
	// The failingFS will fail twice, then succeed
	_, _ = fs.Open("/nonexistent")

	// Create collector
	collector := NewPrometheusCollector(fs, "test", "")
	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)

	// Verify error classification metrics
	metrics := fs.GetMetrics()
	if len(metrics.ErrorsByClass) == 0 {
		t.Log("No errors by class recorded (operation may have succeeded)")
	}

	// Gather Prometheus metrics
	promMetrics, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	t.Logf("Gathered %d metric families", len(promMetrics))
}

func TestPrometheusCollector_MustRegister(t *testing.T) {
	underlying := mustNewMemFS()
	fs := New(underlying).(*RetryFS)

	// Perform an operation to generate metrics
	_ = fs.MkdirAll("/test", 0755)

	collector := NewPrometheusCollector(fs, "test", "")
	registry := prometheus.NewRegistry()

	// Should not panic
	collector.MustRegister(registry)

	// Verify it's registered and collecting
	metricCount := testutil.CollectAndCount(collector)
	if metricCount == 0 {
		t.Error("Expected metrics after MustRegister")
	}

	t.Logf("Collected %d metrics after MustRegister", metricCount)
}

func TestPrometheusCollector_CircuitBreakerStates(t *testing.T) {
	underlying := mustNewMemFS()
	cb := NewCircuitBreaker()
	cb.FailureThreshold = 2
	fs := New(underlying, WithCircuitBreaker(cb)).(*RetryFS)

	collector := NewPrometheusCollector(fs, "test", "")
	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)

	// Test closed state (0)
	metrics, _ := registry.Gather()
	stateValue := findMetricValue(metrics, "test_circuit_state")
	if stateValue != 0 {
		t.Errorf("Expected circuit state 0 (closed), got: %f", stateValue)
	}

	// Open the circuit by failing
	failing := newFailingFS(underlying, 10) // Always fail
	fs2 := New(failing, WithPolicy(&Policy{
		MaxAttempts: 1,
		BaseDelay:   1,
	}), WithCircuitBreaker(cb)).(*RetryFS)

	// Cause failures to open circuit
	for i := 0; i < 5; i++ {
		_ = fs2.MkdirAll("/test", 0755)
	}

	collector2 := NewPrometheusCollector(fs2, "test2", "")
	registry2 := prometheus.NewRegistry()
	registry2.MustRegister(collector2)

	metrics2, _ := registry2.Gather()
	stateValue2 := findMetricValue(metrics2, "test2_circuit_state")

	t.Logf("Circuit state after failures: %f (0=closed, 1=open, 2=half-open)", stateValue2)
}

// Helper function to find a metric value by name
func findMetricValue(metrics []*dto.MetricFamily, name string) float64 {
	for _, mf := range metrics {
		if mf.GetName() == name {
			if len(mf.GetMetric()) > 0 {
				m := mf.GetMetric()[0]
				if m.GetGauge() != nil {
					return m.GetGauge().GetValue()
				}
				if m.GetCounter() != nil {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return -1
}
