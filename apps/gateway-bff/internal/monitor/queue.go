package monitor

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

const notificationTopic = "monitor.notification.requested.v1"
const notificationDLQTopic = "monitor.notification.dead-letter.v1"
const queueDeliveryAttempts = 3

type NotificationTask struct {
	EventID   string `json:"eventId"`
	EventType string `json:"eventType"`
	Alert     Alert  `json:"alert"`
	ChannelID string `json:"channelId"`
}
type QueueStatus struct {
	Enabled        bool   `json:"enabled"`
	Consumer       string `json:"consumer"`
	PendingOutbox  int    `json:"pendingOutbox"`
	DeadLetters    int    `json:"deadLetters"`
	LastConsumedAt string `json:"lastConsumedAt"`
}
type DeadLetter struct {
	ID         string `json:"id"`
	EventID    string `json:"eventId"`
	AlertID    string `json:"alertId"`
	Error      string `json:"error"`
	Attempts   int    `json:"attempts"`
	CreatedAt  string `json:"createdAt"`
	AlertTitle string `json:"alertTitle"`
	ChannelID  string `json:"channelId"`
}
type deadLetterEnvelope struct {
	EventID   string           `json:"eventId"`
	EventType string           `json:"eventType"`
	Original  NotificationTask `json:"original"`
	Error     string           `json:"error"`
	FailedAt  time.Time        `json:"failedAt"`
}

func queueEventID() string {
	data := make([]byte, 12)
	_, _ = rand.Read(data)
	return "notification-" + hex.EncodeToString(data)
}
func enqueueNotification(ctx context.Context, tx *sql.Tx, alert Alert, channelID string) error {
	task := NotificationTask{EventID: queueEventID(), EventType: notificationTopic, Alert: alert, ChannelID: channelID}
	payload, err := json.Marshal(task)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO event_outbox(id,topic,event_key,payload) VALUES($1,$2,$3,$4)`, task.EventID, notificationTopic, alert.ID, payload)
	return err
}
func decodeNotificationTask(payload []byte) (NotificationTask, error) {
	var task NotificationTask
	if err := json.Unmarshal(payload, &task); err != nil {
		return task, err
	}
	if task.EventType != notificationTopic || task.EventID == "" {
		return task, fmt.Errorf("invalid notification task")
	}
	return task, nil
}
func (s *Service) setConsumerStatus(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consumerStatus = status
}
func (s *Service) QueueStatus() QueueStatus {
	result := QueueStatus{Enabled: s.notificationQueue}
	s.mu.RLock()
	result.Consumer = s.consumerStatus
	result.LastConsumedAt = s.lastConsumedAt
	s.mu.RUnlock()
	if s.db != nil {
		_ = s.db.QueryRow(`SELECT count(*) FROM event_outbox WHERE topic=$1 AND published_at IS NULL`, notificationTopic).Scan(&result.PendingOutbox)
		_ = s.db.QueryRow(`SELECT count(*) FROM monitor_notification_dead_letters`).Scan(&result.DeadLetters)
	}
	return result
}
func (s *Service) DeadLetters() []DeadLetter {
	if s.db == nil {
		return []DeadLetter{}
	}
	rows, err := s.db.Query(`SELECT id,event_id,alert_id,payload,error,attempts,created_at FROM monitor_notification_dead_letters ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		return []DeadLetter{}
	}
	defer rows.Close()
	result := []DeadLetter{}
	for rows.Next() {
		var item DeadLetter
		var payload []byte
		var created time.Time
		if rows.Scan(&item.ID, &item.EventID, &item.AlertID, &payload, &item.Error, &item.Attempts, &created) == nil {
			var envelope deadLetterEnvelope
			if json.Unmarshal(payload, &envelope) == nil {
				item.AlertTitle = envelope.Original.Alert.Title
				item.ChannelID = envelope.Original.ChannelID
			}
			item.CreatedAt = created.Local().Format("2006-01-02 15:04:05")
			result = append(result, item)
		}
	}
	return result
}
func (s *Service) moveToDeadLetter(task NotificationTask, deliveryErr error) error {
	if s.db == nil {
		return fmt.Errorf("dead letter database unavailable")
	}
	payload, err := json.Marshal(deadLetterEnvelope{EventID: queueEventID(), EventType: notificationDLQTopic, Original: task, Error: deliveryErr.Error(), FailedAt: time.Now().UTC()})
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id := queueEventID()
	if _, err = tx.Exec(`INSERT INTO monitor_notification_dead_letters(id,event_id,alert_id,payload,error,attempts) VALUES($1,$2,$3,$4,$5,$6)`, id, task.EventID, task.Alert.ID, payload, deliveryErr.Error(), queueDeliveryAttempts); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO event_outbox(id,topic,event_key,payload) VALUES($1,$2,$3,$4)`, queueEventID(), notificationDLQTopic, task.Alert.ID, payload); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) ReplayDeadLetter(id string) error {
	if s.db == nil {
		return fmt.Errorf("dead letter database unavailable")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var payload []byte
	if err = tx.QueryRow(`SELECT payload FROM monitor_notification_dead_letters WHERE id=$1 FOR UPDATE`, id).Scan(&payload); err != nil {
		return err
	}
	var envelope deadLetterEnvelope
	if err = json.Unmarshal(payload, &envelope); err != nil {
		return err
	}
	task := envelope.Original
	task.EventID = queueEventID()
	task.EventType = notificationTopic
	taskPayload, err := json.Marshal(task)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO event_outbox(id,topic,event_key,payload) VALUES($1,$2,$3,$4)`, task.EventID, notificationTopic, task.Alert.ID, taskPayload); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM monitor_notification_dead_letters WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) DeleteDeadLetter(id string) error {
	if s.db == nil {
		return fmt.Errorf("dead letter database unavailable")
	}
	result, err := s.db.Exec(`DELETE FROM monitor_notification_dead_letters WHERE id=$1`, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Service) ReplayDeadLetters(ids []string) (int, []string) {
	success := 0
	failed := []string{}
	for _, id := range ids {
		if err := s.ReplayDeadLetter(id); err != nil {
			failed = append(failed, id)
		} else {
			success++
		}
	}
	return success, failed
}
func (s *Service) DeleteDeadLetters(ids []string) (int, []string) {
	success := 0
	failed := []string{}
	for _, id := range ids {
		if err := s.DeleteDeadLetter(id); err != nil {
			failed = append(failed, id)
		} else {
			success++
		}
	}
	return success, failed
}
func (s *Service) ConsumeNotifications(ctx context.Context, brokers string) {
	if strings.TrimSpace(brokers) == "" {
		return
	}
	s.setConsumerStatus("running")
	defer s.setConsumerStatus("stopped")
	reader := kafka.NewReader(kafka.ReaderConfig{Brokers: strings.Split(brokers, ","), Topic: notificationTopic, GroupID: "cmdb-notification-dispatcher-v1", StartOffset: kafka.FirstOffset, MinBytes: 1, MaxBytes: 10e6, MaxWait: time.Second})
	defer reader.Close()
	for {
		message, err := reader.FetchMessage(ctx)
		if err != nil {
			return
		}
		task, err := decodeNotificationTask(message.Value)
		if err != nil {
			_ = reader.CommitMessages(ctx, message)
			continue
		}
		var deliveryErr error
		for attempt := 1; attempt <= queueDeliveryAttempts; attempt++ {
			if deliveryErr = s.notify(task.Alert, task.ChannelID); deliveryErr == nil {
				break
			}
			if attempt == queueDeliveryAttempts {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
		if deliveryErr != nil {
			if err = s.moveToDeadLetter(task, deliveryErr); err != nil {
				return
			}
		}
		if err = reader.CommitMessages(ctx, message); err != nil {
			return
		}
		s.mu.Lock()
		s.lastConsumedAt = time.Now().Format("2006-01-02 15:04:05")
		s.mu.Unlock()
	}
}
