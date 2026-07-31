package monitor

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type prometheusResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"`
			Values [][]any           `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func (s *Service) Coverage(expected []ExpectedHost) Coverage {
	result := Coverage{Total: len(expected), Hosts: make([]CoverageHost, 0, len(expected))}
	states := map[string]bool{}
	if s.prometheusURL != "" {
		response, err := s.queryPrometheus("/api/v1/query", url.Values{"query": {`up{job="cmdb-node-exporter"}`}})
		if err == nil {
			result.Available = true
			for _, sample := range response.Data.Result {
				assetID := sample.Metric["cmdb_asset_id"]
				value, valueErr := sampleValue(sample.Value)
				if assetID != "" && valueErr == nil {
					states[assetID] = value > 0
				}
			}
		}
	}
	for _, host := range expected {
		item := CoverageHost{AssetID: host.AssetID, Name: host.Name, IP: host.IP, Environment: host.Environment, ProjectGroup: host.ProjectGroup, AssetStatus: host.AssetStatus, MonitorStatus: "missing"}
		if up, found := states[host.AssetID]; found {
			result.Monitored++
			if up {
				item.MonitorStatus = "healthy"
				result.Healthy++
			} else {
				item.MonitorStatus = "down"
				result.Down++
			}
		} else {
			result.Missing++
		}
		result.Hosts = append(result.Hosts, item)
	}
	if result.Total > 0 {
		result.Percentage = math.Round(float64(result.Monitored)/float64(result.Total)*10000) / 100
	}
	return result
}

type metricDefinition struct {
	id, name, target, query, unit string
	warning, critical, multiplier float64
}

func (s *Service) queryPrometheus(path string, params url.Values) (prometheusResponse, error) {
	var result prometheusResponse
	requestURL := s.prometheusURL + path + "?" + params.Encode()
	request, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return result, err
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return result, fmt.Errorf("prometheus returned %s", response.Status)
	}
	err = json.NewDecoder(response.Body).Decode(&result)
	if err == nil && result.Status != "success" {
		err = fmt.Errorf("prometheus query failed")
	}
	return result, err
}
func sampleValue(value []any) (float64, error) {
	if len(value) < 2 {
		return 0, fmt.Errorf("missing sample")
	}
	text, ok := value[1].(string)
	if !ok {
		return 0, fmt.Errorf("invalid sample")
	}
	return strconv.ParseFloat(text, 64)
}
func (s *Service) instant(query string) (float64, error) {
	response, err := s.queryPrometheus("/api/v1/query", url.Values{"query": {query}})
	if err != nil {
		return 0, err
	}
	if len(response.Data.Result) == 0 {
		return 0, fmt.Errorf("prometheus query returned no samples")
	}
	return sampleValue(response.Data.Result[0].Value)
}
func (s *Service) rangeValues(query string, multiplier float64) ([]float64, error) {
	end := time.Now()
	response, err := s.queryPrometheus("/api/v1/query_range", url.Values{"query": {query}, "start": {end.Add(-30 * time.Minute).Format(time.RFC3339)}, "end": {end.Format(time.RFC3339)}, "step": {"5m"}})
	if err != nil || len(response.Data.Result) == 0 {
		return nil, err
	}
	values := make([]float64, 0, len(response.Data.Result[0].Values))
	for _, sample := range response.Data.Result[0].Values {
		value, e := sampleValue(sample)
		if e == nil {
			values = append(values, value*multiplier)
		}
	}
	return values, nil
}

func (s *Service) prometheusHostMetrics(assetID string) ([]Metric, bool, bool) {
	selector := `cmdb_asset_id=` + strconv.Quote(assetID)
	up, err := s.instant(`max(up{job="cmdb-node-exporter",` + selector + `})`)
	if err != nil {
		return []Metric{}, false, false
	}
	definitions := []metricDefinition{
		{"cpu", "CPU 使用率", assetID, `100 - avg(rate(node_cpu_seconds_total{mode="idle",` + selector + `}[5m])) * 100`, "%", 80, 90, 1},
		{"memory", "内存使用率", assetID, `(1 - node_memory_MemAvailable_bytes{` + selector + `} / node_memory_MemTotal_bytes{` + selector + `}) * 100`, "%", 80, 90, 1},
		{"disk", "根分区使用率", assetID, `(1 - node_filesystem_avail_bytes{mountpoint="/",fstype!~"tmpfs|overlay",` + selector + `} / node_filesystem_size_bytes{mountpoint="/",fstype!~"tmpfs|overlay",` + selector + `}) * 100`, "%", 80, 90, 1},
		{"network", "网络吞吐", assetID, `sum(rate(node_network_receive_bytes_total{device!~"lo|veth.*",` + selector + `}[5m]) + rate(node_network_transmit_bytes_total{device!~"lo|veth.*",` + selector + `}[5m])) * 8 / 1000000`, " Mbps", 1000, 5000, 1},
	}
	metrics := make([]Metric, 0, len(definitions))
	for _, definition := range definitions {
		value, queryErr := s.instant(definition.query)
		if queryErr != nil {
			continue
		}
		trend, _ := s.rangeValues(definition.query, definition.multiplier)
		metrics = append(metrics, Metric{ID: definition.id, Name: definition.name, Target: definition.target, Value: math.Round(value*100) / 100, Unit: strings.TrimSpace(definition.unit), Warning: definition.warning, Critical: definition.critical, Trend: trend})
	}
	return metrics, true, up > 0
}

func (s *Service) prometheusMetrics() []Metric {
	definitions := []metricDefinition{
		{"gateway-rps", "Gateway 请求速率", "gateway-bff", `sum(rate(cmdb_http_requests_total[5m]))`, " req/s", 50, 100, 1},
		{"gateway-p95", "Gateway P95 延迟", "gateway-bff", `histogram_quantile(0.95, sum by (le) (rate(cmdb_http_request_duration_seconds_bucket[5m])))`, " ms", 300, 500, 1000},
		{"assets", "CMDB 资产总量", "ops-platform", `cmdb_assets_total`, " 个", 1000000, 2000000, 1},
		{"discovery-pending", "待纳管资源", "discovery", `cmdb_discovery_pending_total`, " 个", 20, 50, 1},
		{"host-cpu", "主机 CPU 使用率", "cmdb-host", `100 - avg(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100`, "%", 80, 90, 1},
		{"host-memory", "主机内存使用率", "cmdb-host", `(1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100`, "%", 80, 90, 1},
		{"host-disk", "根分区使用率", "cmdb-host", `(1 - node_filesystem_avail_bytes{mountpoint="/",fstype!~"tmpfs|overlay"} / node_filesystem_size_bytes{mountpoint="/",fstype!~"tmpfs|overlay"}) * 100`, "%", 80, 90, 1},
		{"host-network", "主机入口流量", "cmdb-host", `sum(rate(node_network_receive_bytes_total{device!~"lo|veth.*"}[5m])) * 8 / 1000000`, " Mbps", 1000, 5000, 1},
		{"postgres-connections", "PostgreSQL 连接数", "postgresql", `sum(pg_stat_database_numbackends)`, " 个", 70, 90, 1},
		{"redis-memory", "Redis 内存使用", "redis", `redis_memory_used_bytes / 1024 / 1024`, " MB", 1024, 2048, 1},
		{"redis-hit-rate", "Redis 缓存命中率", "redis", `rate(redis_keyspace_hits_total[5m]) / clamp_min(rate(redis_keyspace_hits_total[5m]) + rate(redis_keyspace_misses_total[5m]), 0.000001) * 100`, "%", 101, 102, 1},
		{"redis-clients", "Redis 客户端连接", "redis", `redis_connected_clients`, " 个", 500, 1000, 1},
	}
	result := make([]Metric, 0, len(definitions))
	for _, definition := range definitions {
		value, err := s.instant(definition.query)
		if err != nil {
			continue
		}
		trend, _ := s.rangeValues(definition.query, definition.multiplier)
		result = append(result, Metric{ID: definition.id, Name: definition.name, Target: definition.target, Value: math.Round(value*definition.multiplier*100) / 100, Unit: definition.unit, Warning: definition.warning, Critical: definition.critical, Trend: trend})
	}
	return result
}
