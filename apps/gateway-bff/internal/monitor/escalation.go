package monitor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type EscalationPolicy struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Severity       string `json:"severity"`
	TimeoutMinutes int    `json:"timeoutMinutes"`
	Team           string `json:"team"`
	Owner          string `json:"owner"`
	ChannelID      string `json:"channelId"`
	Enabled        bool   `json:"enabled"`
}
type CreateEscalationPolicyInput struct {
	Name           string `json:"name"`
	Severity       string `json:"severity"`
	TimeoutMinutes int    `json:"timeoutMinutes"`
	Team           string `json:"team"`
	Owner          string `json:"owner"`
	ChannelID      string `json:"channelId"`
}

func escalationID() string {
	data := make([]byte, 8)
	_, _ = rand.Read(data)
	return "escalation-" + hex.EncodeToString(data)
}
func validateEscalation(input CreateEscalationPolicyInput) error {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Severity) == "" || input.TimeoutMinutes < 1 || strings.TrimSpace(input.Team) == "" || strings.TrimSpace(input.Owner) == "" {
		return fmt.Errorf("invalid escalation policy")
	}
	return nil
}
func (s *Service) EscalationPolicies() []EscalationPolicy {
	if s.db == nil {
		return []EscalationPolicy{}
	}
	rows, err := s.db.Query(`SELECT id,name,severity,timeout_minutes,team,owner,channel_id,enabled FROM monitor_escalation_policies ORDER BY created_at`)
	if err != nil {
		return []EscalationPolicy{}
	}
	defer rows.Close()
	result := []EscalationPolicy{}
	for rows.Next() {
		var item EscalationPolicy
		if rows.Scan(&item.ID, &item.Name, &item.Severity, &item.TimeoutMinutes, &item.Team, &item.Owner, &item.ChannelID, &item.Enabled) == nil {
			result = append(result, item)
		}
	}
	return result
}
func (s *Service) CreateEscalationPolicy(input CreateEscalationPolicyInput) (EscalationPolicy, error) {
	if err := validateEscalation(input); err != nil {
		return EscalationPolicy{}, err
	}
	if s.db == nil {
		return EscalationPolicy{}, fmt.Errorf("database unavailable")
	}
	if input.ChannelID != "" {
		s.mu.RLock()
		found := false
		for _, channel := range s.channels {
			if channel.ID == input.ChannelID {
				found = true
				break
			}
		}
		s.mu.RUnlock()
		if !found {
			return EscalationPolicy{}, fmt.Errorf("notification channel not found")
		}
	}
	item := EscalationPolicy{ID: escalationID(), Name: strings.TrimSpace(input.Name), Severity: strings.TrimSpace(input.Severity), TimeoutMinutes: input.TimeoutMinutes, Team: strings.TrimSpace(input.Team), Owner: strings.TrimSpace(input.Owner), ChannelID: input.ChannelID, Enabled: true}
	_, err := s.db.Exec(`INSERT INTO monitor_escalation_policies(id,name,severity,timeout_minutes,team,owner,channel_id,enabled) VALUES($1,$2,$3,$4,$5,$6,$7,true)`, item.ID, item.Name, item.Severity, item.TimeoutMinutes, item.Team, item.Owner, item.ChannelID)
	return item, err
}
func (s *Service) ToggleEscalationPolicy(id string) (EscalationPolicy, error) {
	if s.db == nil {
		return EscalationPolicy{}, fmt.Errorf("database unavailable")
	}
	var item EscalationPolicy
	err := s.db.QueryRow(`UPDATE monitor_escalation_policies SET enabled=NOT enabled,updated_at=now() WHERE id=$1 RETURNING id,name,severity,timeout_minutes,team,owner,channel_id,enabled`, id).Scan(&item.ID, &item.Name, &item.Severity, &item.TimeoutMinutes, &item.Team, &item.Owner, &item.ChannelID, &item.Enabled)
	return item, err
}
func (s *Service) DeleteEscalationPolicy(id string) error {
	if s.db == nil {
		return fmt.Errorf("database unavailable")
	}
	result, err := s.db.Exec(`DELETE FROM monitor_escalation_policies WHERE id=$1`, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

type escalationCandidate struct {
	alert  Alert
	policy EscalationPolicy
}

func (s *Service) evaluateEscalations(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.id,a.title,a.severity,a.status,a.target_id,a.target,a.rule,to_char(a.started_at AT TIME ZONE 'UTC','YYYY-MM-DD HH24:MI'),a.duration,a.owner,a.message,p.id,p.name,p.severity,p.timeout_minutes,p.team,p.owner,p.channel_id,p.enabled FROM monitor_alerts a JOIN monitor_escalation_policies p ON p.enabled AND p.severity=a.severity LEFT JOIN monitor_alert_escalations e ON e.alert_id=a.id AND e.policy_id=p.id WHERE a.status='firing' AND a.started_at <= now()-(p.timeout_minutes*interval '1 minute') AND e.alert_id IS NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []escalationCandidate{}
	for rows.Next() {
		var c escalationCandidate
		if err := rows.Scan(&c.alert.ID, &c.alert.Title, &c.alert.Severity, &c.alert.Status, &c.alert.TargetID, &c.alert.Target, &c.alert.Rule, &c.alert.StartedAt, &c.alert.Duration, &c.alert.Owner, &c.alert.Message, &c.policy.ID, &c.policy.Name, &c.policy.Severity, &c.policy.TimeoutMinutes, &c.policy.Team, &c.policy.Owner, &c.policy.ChannelID, &c.policy.Enabled); err != nil {
			return err
		}
		items = append(items, c)
	}
	for _, item := range items {
		if err := s.escalate(ctx, item); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) escalate(ctx context.Context, item escalationCandidate) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO monitor_alert_escalations(alert_id,policy_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, item.alert.ID, item.policy.ID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return nil
	}
	message := "告警超过 " + fmt.Sprint(item.policy.TimeoutMinutes) + " 分钟未确认，已升级到 " + item.policy.Team
	item.alert.Owner = item.policy.Owner
	item.alert.Message = message + "；" + item.alert.Message
	if _, err = tx.ExecContext(ctx, `UPDATE monitor_alerts SET owner=$2,updated_at=now() WHERE id=$1`, item.alert.ID, item.policy.Owner); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO monitor_alert_events(alert_id,event_type,operator,message) VALUES($1,'escalated','system',$2)`, item.alert.ID, message); err != nil {
		return err
	}
	if s.notificationQueue {
		if err = enqueueNotification(ctx, tx, item.alert, item.policy.ChannelID); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.mu.Lock()
	for i := range s.alerts {
		if s.alerts[i].ID == item.alert.ID {
			s.alerts[i].Owner = item.policy.Owner
			break
		}
	}
	event := AlertEvent{ID: -time.Now().UnixNano(), AlertID: item.alert.ID, Type: "escalated", Operator: "system", Message: message, CreatedAt: time.Now().Format("2006-01-02 15:04:05")}
	s.events[item.alert.ID] = append([]AlertEvent{event}, s.events[item.alert.ID]...)
	s.mu.Unlock()
	if !s.notificationQueue {
		go func() { _ = s.notify(item.alert, item.policy.ChannelID) }()
	}
	return nil
}
func (s *Service) RunEscalations(ctx context.Context) {
	if s.db == nil {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		_ = s.evaluateEscalations(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
