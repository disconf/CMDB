package monitor

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"
)

type NotificationDelivery struct {
	ID          string `json:"id"`
	ChannelID   string `json:"channelId"`
	ChannelName string `json:"channelName"`
	AlertID     string `json:"alertId"`
	AlertTitle  string `json:"alertTitle"`
	Attempt     int    `json:"attempt"`
	Status      string `json:"status"`
	HTTPStatus  int    `json:"httpStatus"`
	Error       string `json:"error"`
	CreatedAt   string `json:"createdAt"`
}
type DeliveryStats struct {
	Total       int     `json:"total"`
	Successful  int     `json:"successful"`
	Failed      int     `json:"failed"`
	SuccessRate float64 `json:"successRate"`
}

func deliveryID() string {
	data := make([]byte, 8)
	_, _ = rand.Read(data)
	return "delivery-" + hex.EncodeToString(data)
}
func (s *Service) NotificationDeliveries() []NotificationDelivery {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]NotificationDelivery(nil), s.deliveries...)
}
func (s *Service) NotificationDeliveryStats() DeliveryStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stats := DeliveryStats{Total: len(s.deliveries)}
	for _, item := range s.deliveries {
		if item.Status == "success" {
			stats.Successful++
		} else {
			stats.Failed++
		}
	}
	if stats.Total > 0 {
		stats.SuccessRate = float64(stats.Successful) / float64(stats.Total) * 100
	}
	return stats
}
func (s *Service) recordDelivery(channel NotificationChannel, alert Alert, attempt, statusCode int, sendErr error) {
	item := NotificationDelivery{ID: deliveryID(), ChannelID: channel.ID, ChannelName: channel.Name, AlertID: alert.ID, AlertTitle: alert.Title, Attempt: attempt, HTTPStatus: statusCode, Status: "success", CreatedAt: time.Now().Format("2006-01-02 15:04:05")}
	if sendErr != nil {
		item.Status = "failed"
		item.Error = sendErr.Error()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deliveries = append([]NotificationDelivery{item}, s.deliveries...)
	if len(s.deliveries) > 100 {
		s.deliveries = s.deliveries[:100]
	}
	if s.db != nil {
		_, _ = s.db.Exec(`INSERT INTO monitor_notification_deliveries(id,channel_id,channel_name,alert_id,alert_title,attempt,status,http_status,error) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, item.ID, item.ChannelID, item.ChannelName, item.AlertID, item.AlertTitle, item.Attempt, item.Status, item.HTTPStatus, item.Error)
	}
}
func loadNotificationDeliveries(ctx context.Context, db *sql.DB) ([]NotificationDelivery, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,channel_id,channel_name,alert_id,alert_title,attempt,status,http_status,error,created_at FROM monitor_notification_deliveries ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []NotificationDelivery{}
	for rows.Next() {
		var item NotificationDelivery
		var created time.Time
		if err := rows.Scan(&item.ID, &item.ChannelID, &item.ChannelName, &item.AlertID, &item.AlertTitle, &item.Attempt, &item.Status, &item.HTTPStatus, &item.Error, &created); err != nil {
			return nil, err
		}
		item.CreatedAt = created.Local().Format("2006-01-02 15:04:05")
		result = append(result, item)
	}
	return result, rows.Err()
}
