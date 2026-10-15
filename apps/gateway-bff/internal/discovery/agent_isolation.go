package discovery

import (
	"context"
	"errors"
	"strings"
)

func (s *Service) loadAgentIsolation() error {
	if s.db == nil {
		return nil
	}
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS agent_isolation(id text PRIMARY KEY,reason text NOT NULL DEFAULT '',operator text NOT NULL DEFAULT '',updated_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	rows, err := s.db.Query(`SELECT id,reason FROM agent_isolation`)
	if err != nil {
		return err
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var id, reason string
		if err = rows.Scan(&id, &reason); err != nil {
			return err
		}
		result[id] = reason
	}
	s.mu.Lock()
	s.revokedAgents = result
	s.mu.Unlock()
	return rows.Err()
}
func (s *Service) agentRevoked(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	reason, ok := s.revokedAgents[id]
	return reason, ok
}
func (s *Service) RevokeAgent(id, reason, operator string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("agent id is required")
	}
	if s.db != nil {
		if _, err := s.db.ExecContext(context.Background(), `INSERT INTO agent_isolation(id,reason,operator,updated_at) VALUES($1,$2,$3,now()) ON CONFLICT(id) DO UPDATE SET reason=excluded.reason,operator=excluded.operator,updated_at=now()`, id, reason, operator); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.revokedAgents[id] = reason
	s.mu.Unlock()
	return nil
}
func (s *Service) RestoreAgent(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("agent id is required")
	}
	if s.db != nil {
		if _, err := s.db.ExecContext(context.Background(), `DELETE FROM agent_isolation WHERE id=$1`, id); err != nil {
			return err
		}
	}
	s.mu.Lock()
	delete(s.revokedAgents, id)
	s.mu.Unlock()
	return nil
}
func (s *Service) AgentIsolation() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := map[string]string{}
	for id, reason := range s.revokedAgents {
		result[id] = reason
	}
	return result
}
