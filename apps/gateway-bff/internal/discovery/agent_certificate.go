package discovery

import (
	"context"
	"errors"
	"strings"
	"time"
)

type AgentCertificate struct {
	AgentID     string `json:"agentId"`
	Fingerprint string `json:"fingerprint"`
	Serial      string `json:"serial"`
	NotAfter    string `json:"notAfter"`
	UpdatedAt   string `json:"updatedAt"`
}

func (s *Service) loadAgentCertificates() error {
	if s.db == nil {
		return nil
	}
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS agent_certificates(agent_id text PRIMARY KEY,fingerprint text NOT NULL,serial text NOT NULL DEFAULT '',not_after timestamptz,updated_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	rows, err := s.db.Query(`SELECT agent_id,fingerprint,serial,not_after FROM agent_certificates`)
	if err != nil {
		return err
	}
	defer rows.Close()
	result := map[string]AgentCertificate{}
	for rows.Next() {
		var item AgentCertificate
		var notAfter *time.Time
		if err = rows.Scan(&item.AgentID, &item.Fingerprint, &item.Serial, &notAfter); err != nil {
			return err
		}
		if notAfter != nil {
			item.NotAfter = notAfter.Local().Format("2006-01-02 15:04:05")
		}
		result[item.AgentID] = item
	}
	s.mu.Lock()
	s.agentCertificates = result
	s.mu.Unlock()
	return rows.Err()
}
func normalizeFingerprint(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), ":", ""))
}
func (s *Service) RegisterAgentCertificate(cert AgentCertificate) error {
	cert.AgentID = strings.TrimSpace(cert.AgentID)
	cert.Fingerprint = normalizeFingerprint(cert.Fingerprint)
	if cert.AgentID == "" || cert.Fingerprint == "" {
		return errors.New("agent id and fingerprint are required")
	}
	cert.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	if s.db != nil {
		if _, err := s.db.ExecContext(context.Background(), `INSERT INTO agent_certificates(agent_id,fingerprint,serial,not_after,updated_at) VALUES($1,$2,$3,$4,now()) ON CONFLICT(agent_id) DO UPDATE SET fingerprint=excluded.fingerprint,serial=excluded.serial,not_after=excluded.not_after,updated_at=now()`, cert.AgentID, cert.Fingerprint, cert.Serial, nil); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.agentCertificates[cert.AgentID] = cert
	s.mu.Unlock()
	return nil
}
func (s *Service) AgentCertificateAllowed(agentID, fingerprint string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cert, ok := s.agentCertificates[agentID]
	if !ok {
		return true
	}
	return normalizeFingerprint(fingerprint) == cert.Fingerprint
}
func (s *Service) AgentCertificates() map[string]AgentCertificate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := map[string]AgentCertificate{}
	for id, item := range s.agentCertificates {
		result[id] = item
	}
	return result
}
