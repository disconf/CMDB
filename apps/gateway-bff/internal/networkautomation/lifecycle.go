package networkautomation

import (
	"context"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
)

func (s *Service) ensureLifecycle(anomaly cmdb.NetworkAnomaly, policy Policy) (Lifecycle, error) {
	now := time.Now().Format("2006-01-02 15:04:05")
	s.mu.Lock()
	life, exists := s.lifecycle[anomaly.ID]
	if !exists {
		life = Lifecycle{AnomalyID: anomaly.ID, PolicyID: policy.ID, Status: "open", FirstSeenAt: now, LastSeenAt: now}
	}
	if life.PolicyID == "" {
		life.PolicyID = policy.ID
	}
	life.Title = anomaly.Title
	life.Severity = anomaly.Severity
	life.Target = anomalyTarget(anomaly)
	if life.Status == "resolved" {
		life.Status = "open"
		life.ResolvedAt = ""
		life.EscalatedAt = ""
		life.VerificationJobID = ""
	}
	s.lifecycle[anomaly.ID] = life
	s.mu.Unlock()
	return life, nil
}

func (s *Service) saveLifecycle(item Lifecycle) error {
	s.mu.Lock()
	s.lifecycle[item.AnomalyID] = item
	s.mu.Unlock()
	if s.db != nil {
		return upsertLifecycle(context.Background(), s.db, item)
	}
	return nil
}

func (s *Service) snapshotLifecycle() []Lifecycle {
	if s.db != nil {
		if items, err := loadLifecycle(context.Background(), s.db); err == nil {
			s.mu.Lock()
			s.lifecycle = items
			s.mu.Unlock()
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Lifecycle, 0, len(s.lifecycle))
	for _, item := range s.lifecycle {
		result = append(result, item)
	}
	return result
}
