package observability

import (
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

type requestSeriesKey struct {
	method      string
	route       string
	statusClass string
}

type requestSeries struct {
	count   uint64
	sumNS   uint64
	buckets []uint64
}

type Metrics struct {
	inFlightRequests  atomic.Int64
	panicsTotal       atomic.Uint64
	rateLimitedTotal  atomic.Uint64
	readinessChecks   atomic.Uint64
	readinessFailures atomic.Uint64
	traceExportErrors atomic.Uint64

	mu              sync.RWMutex
	requests        map[requestSeriesKey]*requestSeries
	dependencyReady map[string]bool
}

var requestDurationBuckets = []time.Duration{
	5 * time.Millisecond,
	10 * time.Millisecond,
	25 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
	2500 * time.Millisecond,
	5 * time.Second,
}

func NewMetrics() *Metrics {
	return &Metrics{
		requests:        make(map[requestSeriesKey]*requestSeries),
		dependencyReady: make(map[string]bool),
	}
}

func (m *Metrics) ObserveRequest(duration time.Duration) {
	m.ObserveHTTPRequest("UNKNOWN", "unknown", 0, duration)
}

func (m *Metrics) ObserveHTTPRequest(method, route string, status int, duration time.Duration) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "UNKNOWN"
	}
	route = strings.TrimSpace(route)
	if route == "" {
		route = "unknown"
	}

	key := requestSeriesKey{
		method:      method,
		route:       route,
		statusClass: statusClass(status),
	}

	m.mu.Lock()
	series := m.requests[key]
	if series == nil {
		series = &requestSeries{buckets: make([]uint64, len(requestDurationBuckets))}
		m.requests[key] = series
	}
	series.count++
	if duration > 0 {
		series.sumNS += uint64(duration.Nanoseconds())
	}
	for i, upper := range requestDurationBuckets {
		if duration <= upper {
			series.buckets[i]++
		}
	}
	m.mu.Unlock()
}

func (m *Metrics) IncInFlight() {
	m.inFlightRequests.Add(1)
}

func (m *Metrics) DecInFlight() {
	m.inFlightRequests.Add(-1)
}

func (m *Metrics) IncPanic() {
	m.panicsTotal.Add(1)
}

func (m *Metrics) IncRateLimited() {
	m.rateLimitedTotal.Add(1)
}

func (m *Metrics) IncTraceExportError() {
	m.traceExportErrors.Add(1)
}

func (m *Metrics) ObserveReadiness(ok bool) {
	m.readinessChecks.Add(1)
	if !ok {
		m.readinessFailures.Add(1)
	}
}

func (m *Metrics) SetDependencyReady(name string, ready bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	m.mu.Lock()
	m.dependencyReady[name] = ready
	m.mu.Unlock()
}

func (m *Metrics) Handler() http.Handler {
	return m.HandlerWithDependencies(nil, nil)
}

func (m *Metrics) HandlerWithDependencies(db *sql.DB, redisClient *redis.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		m.writeHTTPMetrics(w)
		m.writeOperationalMetrics(w)
		m.writeDependencyMetrics(w, db, redisClient)
	})
}

func (m *Metrics) writeHTTPMetrics(w http.ResponseWriter) {
	m.mu.RLock()
	keys := make([]requestSeriesKey, 0, len(m.requests))
	for key := range m.requests {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].route != keys[j].route {
			return keys[i].route < keys[j].route
		}
		if keys[i].method != keys[j].method {
			return keys[i].method < keys[j].method
		}
		return keys[i].statusClass < keys[j].statusClass
	})

	snapshot := make(map[requestSeriesKey]requestSeries, len(keys))
	for _, key := range keys {
		series := m.requests[key]
		snapshot[key] = requestSeries{
			count:   series.count,
			sumNS:   series.sumNS,
			buckets: append([]uint64(nil), series.buckets...),
		}
	}
	m.mu.RUnlock()

	_, _ = fmt.Fprintln(w, "# HELP task_api_http_requests_total Total HTTP requests processed by bounded route, method, and status class.")
	_, _ = fmt.Fprintln(w, "# TYPE task_api_http_requests_total counter")
	_, _ = fmt.Fprintln(w, "# HELP task_api_http_request_duration_seconds HTTP request duration histogram.")
	_, _ = fmt.Fprintln(w, "# TYPE task_api_http_request_duration_seconds histogram")

	for _, key := range keys {
		series := snapshot[key]
		labels := metricLabels(key)
		_, _ = fmt.Fprintf(w, "task_api_http_requests_total%s %d\n", labels, series.count)

		for i, upper := range requestDurationBuckets {
			_, _ = fmt.Fprintf(
				w,
				"task_api_http_request_duration_seconds_bucket%s %d\n",
				histogramLabels(key, strconv.FormatFloat(upper.Seconds(), 'f', -1, 64)),
				series.buckets[i],
			)
		}
		_, _ = fmt.Fprintf(
			w,
			"task_api_http_request_duration_seconds_bucket%s %d\n",
			histogramLabels(key, "+Inf"),
			series.count,
		)
		_, _ = fmt.Fprintf(
			w,
			"task_api_http_request_duration_seconds_sum%s %g\n",
			labels,
			float64(series.sumNS)/float64(time.Second),
		)
		_, _ = fmt.Fprintf(w, "task_api_http_request_duration_seconds_count%s %d\n", labels, series.count)
	}
}

func (m *Metrics) writeOperationalMetrics(w http.ResponseWriter) {
	_, _ = fmt.Fprintln(w, "# HELP task_api_http_in_flight_requests Current in-flight HTTP requests.")
	_, _ = fmt.Fprintln(w, "# TYPE task_api_http_in_flight_requests gauge")
	_, _ = fmt.Fprintln(w, "task_api_http_in_flight_requests "+strconv.FormatInt(m.inFlightRequests.Load(), 10))
	_, _ = fmt.Fprintln(w, "# HELP task_api_panics_total Recovered HTTP handler panics.")
	_, _ = fmt.Fprintln(w, "# TYPE task_api_panics_total counter")
	_, _ = fmt.Fprintln(w, "task_api_panics_total "+strconv.FormatUint(m.panicsTotal.Load(), 10))
	_, _ = fmt.Fprintln(w, "# HELP task_api_rate_limited_total Authentication requests rejected by rate limiting.")
	_, _ = fmt.Fprintln(w, "# TYPE task_api_rate_limited_total counter")
	_, _ = fmt.Fprintln(w, "task_api_rate_limited_total "+strconv.FormatUint(m.rateLimitedTotal.Load(), 10))
	_, _ = fmt.Fprintln(w, "# HELP task_api_readiness_checks_total Readiness checks performed.")
	_, _ = fmt.Fprintln(w, "# TYPE task_api_readiness_checks_total counter")
	_, _ = fmt.Fprintln(w, "task_api_readiness_checks_total "+strconv.FormatUint(m.readinessChecks.Load(), 10))
	_, _ = fmt.Fprintln(w, "# HELP task_api_readiness_failures_total Failed readiness checks.")
	_, _ = fmt.Fprintln(w, "# TYPE task_api_readiness_failures_total counter")
	_, _ = fmt.Fprintln(w, "task_api_readiness_failures_total "+strconv.FormatUint(m.readinessFailures.Load(), 10))
	_, _ = fmt.Fprintln(w, "# HELP task_api_trace_export_errors_total OTLP trace export failures or dropped spans.")
	_, _ = fmt.Fprintln(w, "# TYPE task_api_trace_export_errors_total counter")
	_, _ = fmt.Fprintln(w, "task_api_trace_export_errors_total "+strconv.FormatUint(m.traceExportErrors.Load(), 10))
	_, _ = fmt.Fprintln(w, "# HELP task_api_dependency_ready Whether a readiness dependency is currently healthy.")
	_, _ = fmt.Fprintln(w, "# TYPE task_api_dependency_ready gauge")

	m.mu.RLock()
	dependencyNames := make([]string, 0, len(m.dependencyReady))
	for name := range m.dependencyReady {
		dependencyNames = append(dependencyNames, name)
	}
	sort.Strings(dependencyNames)
	for _, name := range dependencyNames {
		value := 0
		if m.dependencyReady[name] {
			value = 1
		}
		_, _ = fmt.Fprintf(w, "task_api_dependency_ready{dependency=%s} %d\n", strconv.Quote(name), value)
	}
	m.mu.RUnlock()
}

func (m *Metrics) writeDependencyMetrics(w http.ResponseWriter, db *sql.DB, redisClient *redis.Client) {
	if db != nil {
		stats := db.Stats()
		_, _ = fmt.Fprintln(w, "# HELP task_api_db_open_connections Current PostgreSQL open connections.")
		_, _ = fmt.Fprintln(w, "# TYPE task_api_db_open_connections gauge")
		_, _ = fmt.Fprintf(w, "task_api_db_open_connections %d\n", stats.OpenConnections)
		_, _ = fmt.Fprintf(w, "task_api_db_in_use_connections %d\n", stats.InUse)
		_, _ = fmt.Fprintf(w, "task_api_db_idle_connections %d\n", stats.Idle)
		_, _ = fmt.Fprintf(w, "task_api_db_wait_count_total %d\n", stats.WaitCount)
		_, _ = fmt.Fprintf(w, "task_api_db_wait_duration_seconds_total %g\n", stats.WaitDuration.Seconds())
	}

	if redisClient != nil {
		stats := redisClient.PoolStats()
		_, _ = fmt.Fprintln(w, "# HELP task_api_redis_pool_hits_total Redis pool hit count.")
		_, _ = fmt.Fprintln(w, "# TYPE task_api_redis_pool_hits_total counter")
		_, _ = fmt.Fprintf(w, "task_api_redis_pool_hits_total %d\n", stats.Hits)
		_, _ = fmt.Fprintf(w, "task_api_redis_pool_misses_total %d\n", stats.Misses)
		_, _ = fmt.Fprintf(w, "task_api_redis_pool_timeouts_total %d\n", stats.Timeouts)
		_, _ = fmt.Fprintf(w, "task_api_redis_pool_total_connections %d\n", stats.TotalConns)
		_, _ = fmt.Fprintf(w, "task_api_redis_pool_idle_connections %d\n", stats.IdleConns)
		_, _ = fmt.Fprintf(w, "task_api_redis_pool_stale_connections_total %d\n", stats.StaleConns)
	}
}

func metricLabels(key requestSeriesKey) string {
	return fmt.Sprintf(
		"{method=%s,route=%s,status_class=%s}",
		strconv.Quote(key.method),
		strconv.Quote(key.route),
		strconv.Quote(key.statusClass),
	)
}

func histogramLabels(key requestSeriesKey, le string) string {
	return fmt.Sprintf(
		"{method=%s,route=%s,status_class=%s,le=%s}",
		strconv.Quote(key.method),
		strconv.Quote(key.route),
		strconv.Quote(key.statusClass),
		strconv.Quote(le),
	)
}

func statusClass(status int) string {
	if status < 100 || status > 599 {
		return "unknown"
	}
	return fmt.Sprintf("%dxx", status/100)
}
