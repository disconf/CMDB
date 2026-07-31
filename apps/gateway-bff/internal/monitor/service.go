package monitor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("alert not found")

type Metric struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Target   string    `json:"target"`
	Value    float64   `json:"value"`
	Unit     string    `json:"unit"`
	Warning  float64   `json:"warning"`
	Critical float64   `json:"critical"`
	Trend    []float64 `json:"trend"`
}
type Alert struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Severity  string `json:"severity"`
	Status    string `json:"status"`
	TargetID  string `json:"targetId"`
	Target    string `json:"target"`
	Rule      string `json:"rule"`
	StartedAt string `json:"startedAt"`
	Duration  string `json:"duration"`
	Owner     string `json:"owner"`
	Message   string `json:"message"`
}
type AlertEvent struct {
	ID        int64  `json:"id"`
	AlertID   string `json:"alertId"`
	Type      string `json:"type"`
	Operator  string `json:"operator"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
}
type Summary struct {
	Targets      int     `json:"targets"`
	Healthy      int     `json:"healthy"`
	Active       int     `json:"active"`
	Critical     int     `json:"critical"`
	Acknowledged int     `json:"acknowledged"`
	Availability float64 `json:"availability"`
}
type HostMonitoring struct {
	AssetID   string   `json:"assetId"`
	Monitored bool     `json:"monitored"`
	Up        bool     `json:"up"`
	Metrics   []Metric `json:"metrics"`
	Alerts    []Alert  `json:"alerts"`
}
type ExpectedHost struct {
	AssetID, Name, IP, Environment, ProjectGroup, AssetStatus string
}
type CoverageHost struct {
	AssetID       string `json:"assetId"`
	Name          string `json:"name"`
	IP            string `json:"ip"`
	Environment   string `json:"environment"`
	ProjectGroup  string `json:"projectGroup"`
	AssetStatus   string `json:"assetStatus"`
	MonitorStatus string `json:"monitorStatus"`
}
type Coverage struct {
	Available  bool           `json:"available"`
	Total      int            `json:"total"`
	Monitored  int            `json:"monitored"`
	Healthy    int            `json:"healthy"`
	Down       int            `json:"down"`
	Missing    int            `json:"missing"`
	Percentage float64        `json:"percentage"`
	Hosts      []CoverageHost `json:"hosts"`
}
type Service struct {
	mu                sync.RWMutex
	metrics           []Metric
	alerts            []Alert
	db                *sql.DB
	prometheusURL     string
	alertmanagerURL   string
	httpClient        *http.Client
	events            map[string][]AlertEvent
	routes            []RoutingRule
	channels          []NotificationChannel
	webhookHosts      map[string]bool
	deliveries        []NotificationDelivery
	notificationQueue bool
	consumerStatus    string
	lastConsumedAt    string
}

type WebhookPayload struct {
	Receiver          string            `json:"receiver"`
	Status            string            `json:"status"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	TruncatedAlerts   int               `json:"truncatedAlerts"`
	Alerts            []struct {
		Status      string            `json:"status"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
		StartsAt    time.Time         `json:"startsAt"`
		Fingerprint string            `json:"fingerprint"`
	} `json:"alerts"`
}

func NewService() *Service {
	s := &Service{metrics: []Metric{{"cpu", "CPU 使用率", "order-service-vm", 72.4, "%", 80, 90, []float64{42, 55, 49, 68, 72}}, {"memory", "内存使用率", "payment-gateway-vm", 91.8, "%", 80, 90, []float64{66, 71, 80, 88, 92}}, {"latency", "接口 P95 延迟", "订单服务", 386, "ms", 300, 500, []float64{180, 210, 260, 330, 386}}, {"db-conn", "数据库连接池", "order-mysql-primary", 84, "%", 75, 90, []float64{61, 65, 72, 78, 84}}}, alerts: []Alert{{"alert-001", "支付网关内存使用率过高", "critical", "firing", "vm-prod-052", "payment-gateway-vm", "MemoryUsage > 90%", "2026-07-15 14:12", "28 分钟", "周敏", "持续超过阈值 15 分钟，可能引发 OOM"}, {"alert-002", "订单接口 P95 延迟升高", "warning", "firing", "service-order", "订单服务", "P95Latency > 300ms", "2026-07-15 14:25", "15 分钟", "王强", "延迟较基线升高 68%"}, {"alert-003", "数据库连接池接近上限", "warning", "acknowledged", "db-prod-001", "order-mysql-primary", "DBPool > 75%", "2026-07-15 13:58", "42 分钟", "吴涛", "已确认，正在检查慢查询"}}}
	s.prometheusURL = strings.TrimRight(os.Getenv("PROMETHEUS_URL"), "/")
	s.alertmanagerURL = strings.TrimRight(os.Getenv("ALERTMANAGER_URL"), "/")
	s.httpClient = &http.Client{Timeout: 3 * time.Second}
	s.events = make(map[string][]AlertEvent)
	s.routes = []RoutingRule{}
	s.channels = []NotificationChannel{}
	s.deliveries = []NotificationDelivery{}
	s.webhookHosts = parseAllowedHosts(os.Getenv("WEBHOOK_ALLOWED_HOSTS"))
	s.notificationQueue = os.Getenv("KAFKA_BROKERS") != ""
	if url := os.Getenv("DATABASE_URL"); url != "" {
		db, alerts, err := openPostgres(url)
		if err != nil {
			panic(fmt.Sprintf("initialize monitor repository: %v", err))
		}
		s.db = db
		s.notificationQueue = s.notificationQueue && db != nil
		s.alerts = alerts
		events, loadErr := loadAlertEvents(context.Background(), db)
		if loadErr != nil {
			panic(fmt.Sprintf("load monitor alert events: %v", loadErr))
		}
		s.events = events
		routes, routeErr := loadRoutingRules(context.Background(), db)
		if routeErr != nil {
			panic(fmt.Sprintf("load monitor routing rules: %v", routeErr))
		}
		s.routes = routes
		channels, channelErr := loadNotificationChannels(context.Background(), db)
		if channelErr != nil {
			panic(fmt.Sprintf("load notification channels: %v", channelErr))
		}
		s.channels = channels
		deliveries, deliveryErr := loadNotificationDeliveries(context.Background(), db)
		if deliveryErr != nil {
			panic(fmt.Sprintf("load notification deliveries: %v", deliveryErr))
		}
		s.deliveries = deliveries
	}
	return s
}
func (s *Service) Metrics() []Metric {
	if s.prometheusURL != "" {
		return s.prometheusMetrics()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Metric(nil), s.metrics...)
}
func (s *Service) Alerts() []Alert {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Alert(nil), s.alerts...)
}
func (s *Service) Host(assetID string) HostMonitoring {
	result := HostMonitoring{AssetID: assetID, Metrics: []Metric{}, Alerts: []Alert{}}
	s.mu.RLock()
	for _, alert := range s.alerts {
		if alert.TargetID == assetID {
			result.Alerts = append(result.Alerts, alert)
		}
	}
	s.mu.RUnlock()
	if s.prometheusURL != "" {
		result.Metrics, result.Monitored, result.Up = s.prometheusHostMetrics(assetID)
	}
	return result
}
func (s *Service) Events(alertID string) ([]AlertEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	found := false
	for _, alert := range s.alerts {
		if alert.ID == alertID {
			found = true
			break
		}
	}
	if !found {
		return nil, ErrNotFound
	}
	return append([]AlertEvent(nil), s.events[alertID]...), nil
}

func (s *Service) appendEventLocked(alertID, eventType, operator, message string) error {
	event := AlertEvent{AlertID: alertID, Type: eventType, Operator: operator, Message: message, CreatedAt: time.Now().Format("2006-01-02 15:04:05")}
	if s.db != nil {
		id, createdAt, err := insertAlertEvent(context.Background(), s.db, event)
		if err != nil {
			return err
		}
		event.ID, event.CreatedAt = id, createdAt.Local().Format("2006-01-02 15:04:05")
	} else {
		event.ID = int64(len(s.events[alertID]) + 1)
	}
	s.events[alertID] = append([]AlertEvent{event}, s.events[alertID]...)
	return nil
}
func (s *Service) Summary() Summary {
	r := Summary{}
	if s.prometheusURL == "" {
		r.Targets = 24
		r.Healthy = 21
		r.Availability = 99.96
	} else {
		total, _ := s.instant(`count(up)`)
		healthy, _ := s.instant(`sum(up)`)
		r.Targets = int(total)
		r.Healthy = int(healthy)
		if total > 0 {
			r.Availability = healthy / total * 100
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.alerts {
		if a.Status != "resolved" {
			r.Active++
		}
		if a.Status == "acknowledged" {
			r.Acknowledged++
		}
		if a.Status != "resolved" && a.Severity == "critical" {
			r.Critical++
		}
	}
	return r
}
func (s *Service) ActiveCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, a := range s.alerts {
		if a.Status != "resolved" {
			count++
		}
	}
	return count
}
func (s *Service) change(id, status, operator string) (Alert, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.alerts {
		if s.alerts[i].ID == id {
			if s.db != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if _, err := s.db.ExecContext(ctx, "UPDATE monitor_alerts SET status=$2,owner=$3,duration=$4,updated_at=now() WHERE id=$1", id, status, operator, time.Now().Format("15:04")+" 操作"); err != nil {
					return Alert{}, err
				}
			}
			s.alerts[i].Status = status
			s.alerts[i].Owner = operator
			s.alerts[i].Duration = time.Now().Format("15:04") + " 操作"
			eventType := "resolved"
			if status == "acknowledged" {
				eventType = "acknowledged"
			}
			if status == "silenced" {
				eventType = "silenced"
			}
			if err := s.appendEventLocked(id, eventType, operator, "告警状态已更新"); err != nil {
				return Alert{}, err
			}
			return s.alerts[i], nil
		}
	}
	return Alert{}, ErrNotFound
}
func (s *Service) Acknowledge(id, operator string) (Alert, error) {
	return s.change(id, "acknowledged", operator)
}
func (s *Service) Resolve(id, operator string) (Alert, error) {
	return s.change(id, "resolved", operator)
}
func (s *Service) Silence(id, operator string) (Alert, error) {
	return s.change(id, "silenced", operator)
}
func (s *Service) Assign(id, owner, operator string) (Alert, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return Alert{}, fmt.Errorf("owner is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.alerts {
		if s.alerts[i].ID != id {
			continue
		}
		if s.db != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := s.db.ExecContext(ctx, "UPDATE monitor_alerts SET owner=$2,updated_at=now() WHERE id=$1", id, owner); err != nil {
				return Alert{}, err
			}
		}
		s.alerts[i].Owner = owner
		if err := s.appendEventLocked(id, "assigned", operator, "负责人已分派给 "+owner); err != nil {
			return Alert{}, err
		}
		return s.alerts[i], nil
	}
	return Alert{}, ErrNotFound
}

func (s *Service) ReceiveWebhook(payload WebhookPayload) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var tx *sql.Tx
	if s.db != nil {
		var err error
		tx, err = s.db.Begin()
		if err != nil {
			return 0, err
		}
		defer tx.Rollback()
	}
	for _, incoming := range payload.Alerts {
		id := incoming.Fingerprint
		if id == "" {
			id = fmt.Sprintf("alertmanager-%d", incoming.StartsAt.UnixNano())
		}
		status := incoming.Status
		if payload.Status == "resolved" {
			status = "resolved"
		}
		startedAt := incoming.StartsAt.Local().Format("2006-01-02 15:04")
		targetID := incoming.Labels["cmdb_asset_id"]
		if targetID == "" {
			targetID = incoming.Labels["instance"]
		}
		alert := Alert{ID: id, Title: incoming.Annotations["summary"], Severity: incoming.Labels["severity"], Status: status, TargetID: targetID, Target: incoming.Labels["instance"], Rule: incoming.Labels["alertname"], StartedAt: startedAt, Duration: "实时", Owner: incoming.Labels["owner"], Message: incoming.Annotations["description"]}
		routeOwner, routeChannelID := s.routeTargetLocked(incoming.Labels)
		if alert.Title == "" {
			alert.Title = alert.Rule
		}
		if alert.Owner == "" {
			alert.Owner = routeOwner
		}
		if alert.Severity == "" {
			alert.Severity = "warning"
		}
		for _, existing := range s.alerts {
			if existing.ID == id && (existing.Status == "acknowledged" || existing.Status == "silenced") && alert.Status == "firing" {
				alert.Status, alert.Owner = existing.Status, existing.Owner
				break
			}
		}
		if tx != nil {
			if err := upsertAlert(context.Background(), tx, alert); err != nil {
				return 0, err
			}
		}
		found := false
		for i := range s.alerts {
			if s.alerts[i].ID == id {
				previous := s.alerts[i].Status
				if (previous == "acknowledged" || previous == "silenced") && alert.Status == "firing" {
					alert.Status, alert.Owner = previous, s.alerts[i].Owner
				}
				s.alerts[i] = alert
				found = true
				eventType := "repeated"
				if alert.Status == "resolved" && previous != "resolved" {
					eventType = "resolved"
				}
				if tx == nil {
					if err := s.appendEventLocked(id, eventType, "alertmanager", alert.Message); err != nil {
						return 0, err
					}
				}
				break
			}
		}
		if !found {
			s.alerts = append([]Alert{alert}, s.alerts...)
			if alert.Status == "firing" {
				if tx != nil && s.notificationQueue {
					if err := enqueueNotification(context.Background(), tx, alert, routeChannelID); err != nil {
						return 0, err
					}
				} else {
					go func() { _ = s.notify(alert, routeChannelID) }()
				}
			}
			if tx == nil {
				if err := s.appendEventLocked(id, "firing", "alertmanager", alert.Message); err != nil {
					return 0, err
				}
			}
		}
	}
	if tx != nil {
		if err := tx.Commit(); err != nil {
			return 0, err
		}
		for _, incoming := range payload.Alerts {
			id := incoming.Fingerprint
			if id == "" {
				id = fmt.Sprintf("alertmanager-%d", incoming.StartsAt.UnixNano())
			}
			eventType := "repeated"
			if incoming.Status == "resolved" || payload.Status == "resolved" {
				eventType = "resolved"
			}
			if len(s.events[id]) == 0 && eventType != "resolved" {
				eventType = "firing"
			}
			if err := s.appendEventLocked(id, eventType, "alertmanager", incoming.Annotations["description"]); err != nil {
				return 0, err
			}
		}
	}
	return len(payload.Alerts), nil
}
