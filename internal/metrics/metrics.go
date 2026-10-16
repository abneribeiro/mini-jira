package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTP request metrics
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "endpoint", "status"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "endpoint"},
	)

	// Database metrics
	dbConnectionsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "db_connections_active",
			Help: "Number of active database connections",
		},
	)

	dbQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "db_query_duration_seconds",
			Help:    "Database query duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation", "table"},
	)

	// Business logic metrics
	usersTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "users_total",
			Help: "Total number of users",
		},
	)

	projectsTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "projects_total",
			Help: "Total number of projects",
		},
	)

	tasksTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "tasks_total",
			Help: "Total number of tasks",
		},
	)

	tasksByStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tasks_by_status",
			Help: "Number of tasks by status",
		},
		[]string{"status"},
	)

	// Auth metrics
	loginAttemptsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "login_attempts_total",
			Help: "Total number of login attempts",
		},
		[]string{"result"},
	)
)

// RecordHTTPRequest records an HTTP request
func RecordHTTPRequest(method, endpoint string, statusCode int, duration time.Duration) {
	httpRequestsTotal.WithLabelValues(method, endpoint, strconv.Itoa(statusCode)).Inc()
	httpRequestDuration.WithLabelValues(method, endpoint).Observe(duration.Seconds())
}

// RecordDBQuery records a database query
func RecordDBQuery(operation, table string, duration time.Duration) {
	dbQueryDuration.WithLabelValues(operation, table).Observe(duration.Seconds())
}

// SetDBConnections sets the number of active database connections
func SetDBConnections(count float64) {
	dbConnectionsActive.Set(count)
}

// SetUsersTotal sets the total number of users
func SetUsersTotal(count float64) {
	usersTotal.Set(count)
}

// SetProjectsTotal sets the total number of projects
func SetProjectsTotal(count float64) {
	projectsTotal.Set(count)
}

// SetTasksTotal sets the total number of tasks
func SetTasksTotal(count float64) {
	tasksTotal.Set(count)
}

// SetTasksByStatus sets the number of tasks by status
func SetTasksByStatus(status string, count float64) {
	tasksByStatus.WithLabelValues(status).Set(count)
}

// RecordLoginAttempt records a login attempt
func RecordLoginAttempt(result string) {
	loginAttemptsTotal.WithLabelValues(result).Inc()
}

// ObserveQuery times one repository call. Usage:
//
//	defer metrics.ObserveQuery("select", "tasks")()
func ObserveQuery(operation, table string) func() {
	start := time.Now()

	return func() {
		RecordDBQuery(operation, table, time.Since(start))
	}
}
