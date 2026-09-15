package cmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

var topologySnapshotSequence uint64

type TopologySnapshot struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Environment string          `json:"environment"`
	NodeCount   int             `json:"nodeCount"`
	EdgeCount   int             `json:"edgeCount"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Operator    string          `json:"operator"`
	CreatedAt   string          `json:"createdAt"`
}

type TopologySnapshotInput struct {
	Kind        string
	Environment string
	NodeCount   int
	EdgeCount   int
	Payload     []byte
	Operator    string
}

func (s *Service) SaveTopologySnapshot(input TopologySnapshotInput) (TopologySnapshot, error) {
	input.Kind = strings.TrimSpace(input.Kind)
	input.Environment = strings.TrimSpace(input.Environment)
	if input.Kind == "" {
		input.Kind = "physical"
	}
	if len(input.Payload) == 0 || !json.Valid(input.Payload) {
		return TopologySnapshot{}, ErrValidation
	}
	snapshot := TopologySnapshot{
		ID: fmt.Sprintf("snap-%d-%d", time.Now().UnixNano(), atomic.AddUint64(&topologySnapshotSequence, 1)), Kind: input.Kind, Environment: input.Environment,
		NodeCount: input.NodeCount, EdgeCount: input.EdgeCount, Payload: append(json.RawMessage(nil), input.Payload...),
		Operator: input.Operator, CreatedAt: time.Now().Format("2006-01-02 15:04:05"),
	}
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.db.ExecContext(ctx, `INSERT INTO cmdb_topology_snapshots(id,kind,environment,node_count,edge_count,payload,operator,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, snapshot.ID, snapshot.Kind, snapshot.Environment, snapshot.NodeCount, snapshot.EdgeCount, []byte(snapshot.Payload), snapshot.Operator, time.Now()); err != nil {
			return TopologySnapshot{}, err
		}
		return snapshot, nil
	}
	s.mu.Lock()
	s.snapshots = append(s.snapshots, snapshot)
	s.mu.Unlock()
	return snapshot, nil
}

func (s *Service) TopologySnapshots(kind, environment string, limit int, includePayload bool) []TopologySnapshot {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		query := `SELECT id,kind,environment,node_count,edge_count,payload,operator,created_at FROM cmdb_topology_snapshots WHERE ($1 = '' OR kind = $1) AND ($2 = '' OR environment = $2) ORDER BY created_at DESC LIMIT $3`
		rows, err := s.db.QueryContext(ctx, query, strings.TrimSpace(kind), strings.TrimSpace(environment), limit)
		if err != nil {
			return []TopologySnapshot{}
		}
		defer rows.Close()
		result := make([]TopologySnapshot, 0, limit)
		for rows.Next() {
			var item TopologySnapshot
			var payload []byte
			var created time.Time
			if err := rows.Scan(&item.ID, &item.Kind, &item.Environment, &item.NodeCount, &item.EdgeCount, &payload, &item.Operator, &created); err != nil {
				continue
			}
			item.CreatedAt = created.Local().Format("2006-01-02 15:04:05")
			if includePayload {
				item.Payload = append(json.RawMessage(nil), payload...)
			}
			result = append(result, item)
		}
		return result
	}
	s.mu.RLock()
	result := make([]TopologySnapshot, 0, len(s.snapshots))
	for _, item := range s.snapshots {
		if kind != "" && item.Kind != kind {
			continue
		}
		if environment != "" && item.Environment != environment {
			continue
		}
		copyItem := item
		if !includePayload {
			copyItem.Payload = nil
		}
		result = append(result, copyItem)
	}
	s.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt > result[j].CreatedAt })
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func (s *Service) TopologySnapshot(id string) (TopologySnapshot, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return TopologySnapshot{}, ErrNotFound
	}
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var item TopologySnapshot
		var payload []byte
		var created time.Time
		err := s.db.QueryRowContext(ctx, `SELECT id,kind,environment,node_count,edge_count,payload,operator,created_at FROM cmdb_topology_snapshots WHERE id=$1`, id).Scan(&item.ID, &item.Kind, &item.Environment, &item.NodeCount, &item.EdgeCount, &payload, &item.Operator, &created)
		if err != nil {
			return TopologySnapshot{}, ErrNotFound
		}
		item.CreatedAt = created.Local().Format("2006-01-02 15:04:05")
		item.Payload = append(json.RawMessage(nil), payload...)
		return item, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.snapshots {
		if item.ID == id {
			return item, nil
		}
	}
	return TopologySnapshot{}, ErrNotFound
}
