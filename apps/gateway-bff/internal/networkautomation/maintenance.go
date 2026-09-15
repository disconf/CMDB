package networkautomation

import (
	"context"
	"time"

	"cmdb/gateway-bff/internal/monitor"
)

type ComplianceMaintenanceReport struct {
	Checked  int      `json:"checked"`
	Reminded int      `json:"reminded"`
	Expired  int      `json:"expired"`
	Alerts   int      `json:"alerts"`
	Errors   []string `json:"errors,omitempty"`
}

func (s *Service) RunComplianceMaintenance() ComplianceMaintenanceReport {
	report := ComplianceMaintenanceReport{}
	now := time.Now()
	alerts := []monitor.CMDBComplianceAlert{}
	for _, rule := range s.ComplianceRules() {
		if rule.Action != "temporary" || rule.Status != "approved" {
			continue
		}
		report.Checked++
		expiry, err := parseComplianceTime(rule.ExpiresAt)
		if err != nil {
			continue
		}
		if !expiry.After(now) {
			rule.Status = "expired"
			rule.UpdatedAt = now.Format("2006-01-02 15:04:05")
			if saveErr := s.saveComplianceRule(rule); saveErr != nil {
				report.Errors = append(report.Errors, saveErr.Error())
			}
			report.Expired++
			continue
		}
		if rule.ReminderHours > 0 && expiry.Sub(now) <= time.Duration(rule.ReminderHours)*time.Hour {
			lastReminded, _ := parseComplianceTime(rule.LastRemindedAt)
			if rule.LastRemindedAt == "" || now.Sub(lastReminded) >= 24*time.Hour {
				rule.LastRemindedAt = now.Format("2006-01-02 15:04:05")
				rule.UpdatedAt = rule.LastRemindedAt
				if saveErr := s.saveComplianceRule(rule); saveErr != nil {
					report.Errors = append(report.Errors, saveErr.Error())
				}
				report.Reminded++
			}
			alerts = append(alerts, monitor.CMDBComplianceAlert{ID: "compliance-expiry-" + rule.ID, Title: "网络临时授权即将到期", Severity: "warning", Target: rule.Value, Message: "规则 " + rule.Name + " 将于 " + expiry.Format("2006-01-02 15:04:05") + " 到期", Owner: rule.CreatedBy})
		}
	}
	count, err := s.monitor.SyncCMDBComplianceAlerts(alerts)
	report.Alerts = count
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
	}
	return report
}

func (s *Service) saveComplianceRule(rule ComplianceRule) error {
	if s.db != nil {
		if err := upsertComplianceRule(context.Background(), s.db, rule); err != nil {
			return err
		}
	}
	s.mu.Lock()
	for i := range s.complianceRules {
		if s.complianceRules[i].ID == rule.ID {
			s.complianceRules[i] = rule
		}
	}
	s.mu.Unlock()
	return nil
}

func parseComplianceTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, ErrValidation
	}
	if parsed, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.Local); err == nil {
		return parsed, nil
	}
	return time.ParseInLocation("2006-01-02", value, time.Local)
}
