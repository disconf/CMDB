package monitor

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type NotificationChannel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Endpoint   string `json:"-"`
	DisplayURL string `json:"displayUrl"`
	Enabled    bool   `json:"enabled"`
	LastStatus string `json:"lastStatus"`
	LastError  string `json:"lastError"`
	LastSentAt string `json:"lastSentAt"`
	Template   string `json:"template"`
}
type CreateChannelInput struct {
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	Template string `json:"template"`
}

const defaultNotificationTemplate = "[{{severity}}] {{title}}\n对象: {{target}}\n状态: {{status}}\n负责人: {{owner}}\n规则: {{rule}}\n详情: {{message}}"

func parseAllowedHosts(value string) map[string]bool {
	result := map[string]bool{}
	for _, host := range strings.Split(value, ",") {
		host = strings.ToLower(strings.TrimSpace(host))
		if host != "" {
			result[host] = true
		}
	}
	return result
}
func channelID() string {
	data := make([]byte, 8)
	_, _ = rand.Read(data)
	return "channel-" + hex.EncodeToString(data)
}
func displayURL(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
func (s *Service) validateEndpoint(endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("invalid webhook URL")
	}
	host := strings.ToLower(parsed.Hostname())
	if !s.webhookHosts[host] && !s.webhookHosts[strings.ToLower(parsed.Host)] {
		return fmt.Errorf("webhook host is not allowed")
	}
	return nil
}
func (s *Service) NotificationChannels() []NotificationChannel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := append([]NotificationChannel(nil), s.channels...)
	for i := range result {
		result[i].DisplayURL = displayURL(result[i].Endpoint)
		result[i].Endpoint = ""
	}
	return result
}
func (s *Service) CreateNotificationChannel(input CreateChannelInput) (NotificationChannel, error) {
	if strings.TrimSpace(input.Name) == "" {
		return NotificationChannel{}, fmt.Errorf("channel name is required")
	}
	if err := s.validateEndpoint(input.Endpoint); err != nil {
		return NotificationChannel{}, err
	}
	if len(input.Template) > 4000 {
		return NotificationChannel{}, fmt.Errorf("template is too long")
	}
	if strings.TrimSpace(input.Template) == "" {
		input.Template = defaultNotificationTemplate
	}
	channel := NotificationChannel{ID: channelID(), Name: strings.TrimSpace(input.Name), Endpoint: input.Endpoint, DisplayURL: displayURL(input.Endpoint), Template: input.Template, Enabled: true}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		if _, err := s.db.Exec(`INSERT INTO monitor_notification_channels(id,name,endpoint,template,enabled) VALUES($1,$2,$3,$4,$5)`, channel.ID, channel.Name, channel.Endpoint, channel.Template, true); err != nil {
			return NotificationChannel{}, err
		}
	}
	s.channels = append(s.channels, channel)
	channel.Endpoint = ""
	return channel, nil
}
func renderNotificationTemplate(template string, alert Alert) string {
	if strings.TrimSpace(template) == "" {
		template = defaultNotificationTemplate
	}
	return strings.NewReplacer("{{title}}", alert.Title, "{{severity}}", alert.Severity, "{{target}}", alert.Target, "{{owner}}", alert.Owner, "{{rule}}", alert.Rule, "{{status}}", alert.Status, "{{message}}", alert.Message).Replace(template)
}
func (s *Service) UpdateNotificationTemplate(id, template string) (NotificationChannel, error) {
	if strings.TrimSpace(template) == "" || len(template) > 4000 {
		return NotificationChannel{}, fmt.Errorf("invalid template")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.channels {
		if s.channels[i].ID == id {
			if s.db != nil {
				if _, err := s.db.Exec(`UPDATE monitor_notification_channels SET template=$2,updated_at=now() WHERE id=$1`, id, template); err != nil {
					return NotificationChannel{}, err
				}
			}
			s.channels[i].Template = template
			result := s.channels[i]
			result.DisplayURL = displayURL(result.Endpoint)
			result.Endpoint = ""
			return result, nil
		}
	}
	return NotificationChannel{}, ErrNotFound
}
func (s *Service) ToggleNotificationChannel(id string) (NotificationChannel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.channels {
		if s.channels[i].ID == id {
			s.channels[i].Enabled = !s.channels[i].Enabled
			if s.db != nil {
				if _, err := s.db.Exec(`UPDATE monitor_notification_channels SET enabled=$2,updated_at=now() WHERE id=$1`, id, s.channels[i].Enabled); err != nil {
					return NotificationChannel{}, err
				}
			}
			result := s.channels[i]
			result.DisplayURL = displayURL(result.Endpoint)
			result.Endpoint = ""
			return result, nil
		}
	}
	return NotificationChannel{}, ErrNotFound
}
func (s *Service) DeleteNotificationChannel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.channels {
		if s.channels[i].ID == id {
			if s.db != nil {
				if _, err := s.db.Exec(`DELETE FROM monitor_notification_channels WHERE id=$1`, id); err != nil {
					return err
				}
			}
			s.channels = append(s.channels[:i], s.channels[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}
func (s *Service) TestNotificationChannel(id string) error {
	s.mu.RLock()
	var channel *NotificationChannel
	for i := range s.channels {
		if s.channels[i].ID == id {
			copy := s.channels[i]
			channel = &copy
			break
		}
	}
	s.mu.RUnlock()
	if channel == nil {
		return ErrNotFound
	}
	return s.sendNotification(*channel, Alert{Title: "CMDB 通知渠道测试", Severity: "info", Status: "test", Message: "渠道连通性测试成功"})
}
func (s *Service) notify(alert Alert, channelID string) error {
	s.mu.RLock()
	channels := append([]NotificationChannel(nil), s.channels...)
	s.mu.RUnlock()
	var lastErr error
	for _, channel := range channels {
		if channel.Enabled && (channelID == "" || channel.ID == channelID) {
			if err := s.sendNotification(channel, alert); err != nil {
				lastErr = err
			}
		}
	}
	return lastErr
}
func (s *Service) sendNotification(channel NotificationChannel, alert Alert) error {
	payload, _ := json.Marshal(map[string]any{"event": "cmdb.alert", "text": renderNotificationTemplate(channel.Template, alert), "alert": alert, "sentAt": time.Now().UTC()})
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		statusCode, err := s.deliverOnce(channel.Endpoint, payload)
		s.recordDelivery(channel, alert, attempt, statusCode, err)
		if err == nil {
			s.recordChannelResult(channel.ID, nil)
			return nil
		}
		lastErr = err
		if attempt < 3 {
			time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
		}
	}
	s.recordChannelResult(channel.ID, lastErr)
	return lastErr
}
func (s *Service) deliverOnce(endpoint string, payload []byte) (int, error) {
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err == nil {
		request.Header.Set("Content-Type", "application/json")
		response, e := s.httpClient.Do(request)
		err = e
		if response != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
			response.Body.Close()
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				err = fmt.Errorf("webhook returned %s", response.Status)
			}
			return response.StatusCode, err
		}
	}
	return 0, err
}
func (s *Service) recordChannelResult(id string, sendErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status, errorText := "success", ""
	if sendErr != nil {
		status, errorText = "failed", sendErr.Error()
	}
	now := time.Now()
	for i := range s.channels {
		if s.channels[i].ID == id {
			s.channels[i].LastStatus = status
			s.channels[i].LastError = errorText
			s.channels[i].LastSentAt = now.Format("2006-01-02 15:04:05")
			break
		}
	}
	if s.db != nil {
		_, _ = s.db.Exec(`UPDATE monitor_notification_channels SET last_status=$2,last_error=$3,last_sent_at=$4,updated_at=now() WHERE id=$1`, id, status, errorText, now)
	}
}
func loadNotificationChannels(ctx context.Context, db *sql.DB) ([]NotificationChannel, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,name,endpoint,template,enabled,last_status,last_error,last_sent_at FROM monitor_notification_channels ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []NotificationChannel{}
	for rows.Next() {
		var item NotificationChannel
		var sent sql.NullTime
		if err := rows.Scan(&item.ID, &item.Name, &item.Endpoint, &item.Template, &item.Enabled, &item.LastStatus, &item.LastError, &sent); err != nil {
			return nil, err
		}
		item.DisplayURL = displayURL(item.Endpoint)
		if sent.Valid {
			item.LastSentAt = sent.Time.Local().Format("2006-01-02 15:04:05")
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
