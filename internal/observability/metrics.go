package observability

import (
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

type Metrics struct {
	requestsTotal       atomic.Uint64
	requestDurationNS   atomic.Uint64
	inFlightRequests    atomic.Int64
	panicsTotal         atomic.Uint64
	rateLimitedTotal    atomic.Uint64
	readinessChecks     atomic.Uint64
	readinessFailures   atomic.Uint64
}

func NewMetrics() *Metrics {
	return &Metrics{}
}

func (m *Metrics) ObserveRequest(duration time.Duration) {
	m.requestsTotal.Add(1)
	m.requestDurationNS.Add(uint64(duration.Nanoseconds()))
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

func (m *Metrics) ObserveReadiness(ok bool) {
	m.readinessChecks.Add(1)
	if !ok {
		m.readinessFailures.Add(1)
	}
}

func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		requests := m.requestsTotal.Load()
		durationSeconds := float64(m.requestDurationNS.Load()) / float64(time.Second)

		_, _ = fmt.Fprintln(w, "# HELP task_api_http_requests_total Total HTTP requests processed.")
		_, _ = fmt.Fprintln(w, "# TYPE task_api_http_requests_total counter")
		_, _ = fmt.Fprintln(w, "task_api_http_requests_total "+strconv.FormatUint(requests, 10))
		_, _ = fmt.Fprintln(w, "# HELP task_api_http_request_duration_seconds Total request processing time.")
		_, _ = fmt.Fprintln(w, "# TYPE task_api_http_request_duration_seconds summary")
		_, _ = fmt.Fprintf(w, "task_api_http_request_duration_seconds_sum %g\n", durationSeconds)
		_, _ = fmt.Fprintln(w, "task_api_http_request_duration_seconds_count "+strconv.FormatUint(requests, 10))
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
	})
}
