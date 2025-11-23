package retryfs

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// PrometheusCollector collects retryfs metrics for Prometheus
type PrometheusCollector struct {
	retryFS *RetryFS
	mu      sync.RWMutex

	// Metrics descriptors
	attemptsTotal       *prometheus.Desc
	retriesTotal        *prometheus.Desc
	successesTotal      *prometheus.Desc
	failuresTotal       *prometheus.Desc
	errorsByClass       *prometheus.Desc
	circuitState        *prometheus.Desc
	operationDuration   *prometheus.Desc
	consecutiveErrors   *prometheus.Desc
	consecutiveSuccesses *prometheus.Desc
}

// NewPrometheusCollector creates a new Prometheus collector for the given RetryFS
func NewPrometheusCollector(rfs *RetryFS, namespace, subsystem string) *PrometheusCollector {
	if namespace == "" {
		namespace = "retryfs"
	}

	return &PrometheusCollector{
		retryFS: rfs,
		attemptsTotal: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "attempts_total"),
			"Total number of filesystem operation attempts",
			[]string{"operation"},
			nil,
		),
		retriesTotal: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "retries_total"),
			"Total number of retry attempts",
			[]string{"operation"},
			nil,
		),
		successesTotal: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "successes_total"),
			"Total number of successful operations",
			[]string{"operation"},
			nil,
		),
		failuresTotal: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "failures_total"),
			"Total number of failed operations after all retries",
			[]string{"operation"},
			nil,
		),
		errorsByClass: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "errors_total"),
			"Total number of errors by classification",
			[]string{"class"},
			nil,
		),
		circuitState: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "circuit_state"),
			"Circuit breaker state (0=closed, 1=open, 2=half-open)",
			nil,
			nil,
		),
		operationDuration: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "operation_duration_seconds"),
			"Duration of filesystem operations in seconds",
			[]string{"operation", "result"},
			nil,
		),
		consecutiveErrors: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "circuit_consecutive_errors"),
			"Number of consecutive errors in circuit breaker",
			nil,
			nil,
		),
		consecutiveSuccesses: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "circuit_consecutive_successes"),
			"Number of consecutive successes in circuit breaker",
			nil,
			nil,
		),
	}
}

// Describe implements prometheus.Collector
func (c *PrometheusCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.attemptsTotal
	ch <- c.retriesTotal
	ch <- c.successesTotal
	ch <- c.failuresTotal
	ch <- c.errorsByClass
	ch <- c.circuitState
	ch <- c.consecutiveErrors
	ch <- c.consecutiveSuccesses
}

// Collect implements prometheus.Collector
func (c *PrometheusCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	metrics := c.retryFS.GetMetrics()

	// Per-operation metrics
	for op, opMetrics := range metrics.OperationMetrics {
		ch <- prometheus.MustNewConstMetric(
			c.attemptsTotal,
			prometheus.CounterValue,
			float64(opMetrics.Attempts),
			string(op),
		)
		ch <- prometheus.MustNewConstMetric(
			c.retriesTotal,
			prometheus.CounterValue,
			float64(opMetrics.Retries),
			string(op),
		)
		ch <- prometheus.MustNewConstMetric(
			c.successesTotal,
			prometheus.CounterValue,
			float64(opMetrics.Successes),
			string(op),
		)
		ch <- prometheus.MustNewConstMetric(
			c.failuresTotal,
			prometheus.CounterValue,
			float64(opMetrics.Failures),
			string(op),
		)
	}

	// Errors by class
	for class, count := range metrics.ErrorsByClass {
		ch <- prometheus.MustNewConstMetric(
			c.errorsByClass,
			prometheus.CounterValue,
			float64(count),
			class.String(),
		)
	}

	// Circuit breaker metrics
	if c.retryFS.circuitBreaker != nil {
		stats := c.retryFS.circuitBreaker.GetStats()

		// Map state to numeric value
		var stateValue float64
		switch stats.State {
		case StateClosed:
			stateValue = 0
		case StateOpen:
			stateValue = 1
		case StateHalfOpen:
			stateValue = 2
		}

		ch <- prometheus.MustNewConstMetric(
			c.circuitState,
			prometheus.GaugeValue,
			stateValue,
		)

		ch <- prometheus.MustNewConstMetric(
			c.consecutiveErrors,
			prometheus.GaugeValue,
			float64(stats.ConsecutiveErrors),
		)

		ch <- prometheus.MustNewConstMetric(
			c.consecutiveSuccesses,
			prometheus.GaugeValue,
			float64(stats.ConsecutiveSuccess),
		)
	}
}

// MustRegister registers the collector with the provided Prometheus registry
// It panics if registration fails
func (c *PrometheusCollector) MustRegister(registry *prometheus.Registry) {
	registry.MustRegister(c)
}

// Register registers the collector with the provided Prometheus registry
// Returns an error if registration fails
func (c *PrometheusCollector) Register(registry *prometheus.Registry) error {
	return registry.Register(c)
}
