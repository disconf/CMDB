package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/discovery"
	"cmdb/gateway-bff/internal/monitor"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type metricsMiddleware struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	handler  http.Handler
}
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func newMetrics(cmdbService *cmdb.Service, discoveryService *discovery.Service, monitorService *monitor.Service) *metricsMiddleware {
	registry := prometheus.NewRegistry()
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cmdb_http_requests_total", Help: "Total CMDB gateway HTTP requests."}, []string{"method", "route", "status"})
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "cmdb_http_request_duration_seconds", Help: "CMDB gateway request duration.", Buckets: prometheus.DefBuckets}, []string{"method", "route"})
	registry.MustRegister(requests, duration)
	registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "cmdb_assets_total", Help: "Current number of CMDB assets."}, func() float64 { return float64(cmdbService.Summary().Total) }))
	registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "cmdb_discovery_pending_total", Help: "Discovered resources waiting for reconciliation."}, func() float64 { return float64(discoveryService.Summary().Pending) }))
	registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "cmdb_alerts_active_total", Help: "Current active alerts."}, func() float64 { return float64(monitorService.ActiveCount()) }))
	registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "cmdb_notification_outbox_pending", Help: "Notification events waiting in the transactional outbox."}, func() float64 { return float64(monitorService.QueueStatus().PendingOutbox) }))
	registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "cmdb_notification_dead_letters_total", Help: "Notification events moved to the dead letter queue."}, func() float64 { return float64(monitorService.QueueStatus().DeadLetters) }))
	return &metricsMiddleware{requests: requests, duration: duration, handler: promhttp.HandlerFor(registry, promhttp.HandlerOpts{})}
}
func (m *metricsMiddleware) Handler() http.Handler { return m.handler }
func (m *metricsMiddleware) Instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		route := r.Pattern
		if route == "" {
			route = r.URL.Path
		}
		m.requests.WithLabelValues(r.Method, route, strconv.Itoa(sw.status)).Inc()
		m.duration.WithLabelValues(r.Method, route).Observe(time.Since(started).Seconds())
	})
}
