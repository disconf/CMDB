package httpapi

import (
	"crypto/tls"
	"net"
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

type dependencyProbe struct {
	name    string
	address string
	url     string
}

func newMetrics(cmdbService *cmdb.Service, discoveryService *discovery.Service, monitorService *monitor.Service) *metricsMiddleware {
	registry := prometheus.NewRegistry()
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cmdb_http_requests_total", Help: "Total CMDB gateway HTTP requests."}, []string{"method", "route", "status"})
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "cmdb_http_request_duration_seconds", Help: "CMDB gateway request duration.", Buckets: prometheus.DefBuckets}, []string{"method", "route"})
	dependencies := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "cmdb_dependency_up", Help: "Whether a platform dependency is reachable."}, []string{"name"})
	registry.MustRegister(requests, duration, dependencies)
	registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "cmdb_assets_total", Help: "Current number of CMDB assets."}, func() float64 { return float64(cmdbService.Summary().Total) }))
	registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "cmdb_discovery_pending_total", Help: "Discovered resources waiting for reconciliation."}, func() float64 { return float64(discoveryService.Summary().Pending) }))
	registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "cmdb_alerts_active_total", Help: "Current active alerts."}, func() float64 { return float64(monitorService.ActiveCount()) }))
	registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "cmdb_notification_outbox_pending", Help: "Notification events waiting in the transactional outbox."}, func() float64 { return float64(monitorService.QueueStatus().PendingOutbox) }))
	registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "cmdb_notification_dead_letters_total", Help: "Notification events moved to the dead letter queue."}, func() float64 { return float64(monitorService.QueueStatus().DeadLetters) }))
	go probeDependencies(dependencies)
	return &metricsMiddleware{requests: requests, duration: duration, handler: promhttp.HandlerFor(registry, promhttp.HandlerOpts{})}
}

func probeDependencies(gauge *prometheus.GaugeVec) {
	probes := []dependencyProbe{
		{name: "postgresql", address: "postgres-ha-rw.ops-qa.svc.cluster.local:5432"},
		{name: "keycloak", url: "http://keycloak.ops-qa.svc.cluster.local:8080/realms/cmdb"},
		{name: "redis", address: "redis-cluster-leader.ops-qa.svc.cluster.local:6379"},
		{name: "kafka", address: "kafka.ops-qa.svc.cluster.local:9092"},
		{name: "minio", url: "https://minio.ops-qa.svc.cluster.local/minio/health/live"},
		{name: "vault", url: "http://vault-active.ops-qa.svc.cluster.local:8200/v1/sys/health"},
		{name: "grafana", url: "http://grafana.monitoring.svc.cluster.local:3000/api/health"},
		{name: "snmp-exporter", url: "http://snmp-exporter.monitoring.svc.cluster.local:9116/-/healthy"},
	}
	for _, probe := range probes {
		gauge.WithLabelValues(probe.name).Set(0)
	}
	runProbes := func() {
		for _, probe := range probes {
			up := 0.0
			if dependencyReachable(probe) {
				up = 1
			}
			gauge.WithLabelValues(probe.name).Set(up)
		}
	}
	runProbes()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		runProbes()
	}
}

func dependencyReachable(probe dependencyProbe) bool {
	if probe.address != "" {
		connection, err := net.DialTimeout("tcp", probe.address, 3*time.Second)
		if err != nil {
			return false
		}
		_ = connection.Close()
		return true
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	response, err := client.Get(probe.url)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode >= 200 && response.StatusCode < 500
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
