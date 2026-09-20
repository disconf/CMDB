package cicd

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode"
)

var ErrRelationValidation = errors.New("relation validation")

type ResourceRelation struct {
	ID           string `json:"id"`
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
	ResourceName string `json:"resourceName"`
	AssetID      string `json:"assetId"`
	AssetName    string `json:"assetName"`
	AssetType    string `json:"assetType"`
	RelationType string `json:"relationType"`
	Source       string `json:"source"`
	Actor        string `json:"actor,omitempty"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

type RelationInput struct {
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
	ResourceName string `json:"resourceName"`
	AssetID      string `json:"assetId"`
	AssetName    string `json:"assetName"`
	AssetType    string `json:"assetType"`
	RelationType string `json:"relationType"`
	Source       string `json:"source"`
}

type AssetCandidate struct {
	ID           string
	Name         string
	Type         string
	IP           string
	ProjectGroup string
}

type RelationSuggestion struct {
	AssetID   string `json:"assetId"`
	AssetName string `json:"assetName"`
	AssetType string `json:"assetType"`
	AssetIP   string `json:"assetIp,omitempty"`
	Score     int    `json:"score"`
	Reason    string `json:"reason"`
}

func (s *Service) initRelationStore(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS cicd_resource_relations (
			id TEXT PRIMARY KEY,
			resource_type TEXT NOT NULL,
			resource_id TEXT NOT NULL,
			resource_name TEXT NOT NULL,
			asset_id TEXT NOT NULL,
			asset_name TEXT NOT NULL,
			asset_type TEXT NOT NULL DEFAULT '',
			relation_type TEXT NOT NULL DEFAULT 'related-to',
			source TEXT NOT NULL DEFAULT 'manual',
			actor TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE (resource_type, resource_id, asset_id, relation_type)
		)
	`); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_cicd_resource_relations_resource ON cicd_resource_relations (resource_type, resource_id)`)
	return err
}

func (s *Service) ListRelations(ctx context.Context, resourceType string) []ResourceRelation {
	resourceType = strings.TrimSpace(resourceType)
	if s.db == nil {
		s.relationMu.RLock()
		defer s.relationMu.RUnlock()
		result := make([]ResourceRelation, 0, len(s.relations))
		for _, item := range s.relations {
			if resourceType == "" || item.ResourceType == resourceType {
				result = append(result, item)
			}
		}
		sortRelations(result)
		return result
	}

	queryContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(queryContext, `
		SELECT id, resource_type, resource_id, resource_name, asset_id, asset_name, asset_type, relation_type, source, actor, created_at, updated_at
		FROM cicd_resource_relations
		WHERE ($1 = '' OR resource_type = $1)
		ORDER BY updated_at DESC
	`, resourceType)
	if err != nil {
		return []ResourceRelation{}
	}
	defer rows.Close()
	result := make([]ResourceRelation, 0)
	for rows.Next() {
		item, err := scanRelation(rows)
		if err != nil {
			return []ResourceRelation{}
		}
		result = append(result, item)
	}
	return result
}

func (s *Service) CreateRelation(ctx context.Context, input RelationInput, actor string) (ResourceRelation, error) {
	input.ResourceType = strings.TrimSpace(input.ResourceType)
	input.ResourceID = strings.TrimSpace(input.ResourceID)
	input.ResourceName = strings.TrimSpace(input.ResourceName)
	input.AssetID = strings.TrimSpace(input.AssetID)
	input.AssetName = strings.TrimSpace(input.AssetName)
	input.AssetType = strings.TrimSpace(input.AssetType)
	input.RelationType = strings.TrimSpace(input.RelationType)
	input.Source = strings.TrimSpace(input.Source)
	if input.ResourceType == "" || input.ResourceID == "" || input.ResourceName == "" || input.AssetID == "" || input.AssetName == "" {
		return ResourceRelation{}, ErrRelationValidation
	}
	if input.RelationType == "" {
		input.RelationType = "related-to"
	}
	if input.Source == "" {
		input.Source = "manual"
	}
	now := time.Now().Format(time.RFC3339)
	item := ResourceRelation{
		ID: newRelationID(), ResourceType: input.ResourceType, ResourceID: input.ResourceID, ResourceName: input.ResourceName,
		AssetID: input.AssetID, AssetName: input.AssetName, AssetType: input.AssetType, RelationType: input.RelationType,
		Source: input.Source, Actor: strings.TrimSpace(actor), CreatedAt: now, UpdatedAt: now,
	}
	if s.db == nil {
		s.relationMu.Lock()
		defer s.relationMu.Unlock()
		for index, existing := range s.relations {
			if existing.ResourceType == item.ResourceType && existing.ResourceID == item.ResourceID && existing.AssetID == item.AssetID && existing.RelationType == item.RelationType {
				item.ID = existing.ID
				item.CreatedAt = existing.CreatedAt
				s.relations[index] = item
				return item, nil
			}
		}
		s.relations = append(s.relations, item)
		return item, nil
	}

	queryContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(queryContext, `
		INSERT INTO cicd_resource_relations
			(id, resource_type, resource_id, resource_name, asset_id, asset_name, asset_type, relation_type, source, actor, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (resource_type, resource_id, asset_id, relation_type) DO UPDATE SET
			resource_name = EXCLUDED.resource_name,
			asset_name = EXCLUDED.asset_name,
			asset_type = EXCLUDED.asset_type,
			source = EXCLUDED.source,
			actor = EXCLUDED.actor,
			updated_at = EXCLUDED.updated_at
		RETURNING id, resource_type, resource_id, resource_name, asset_id, asset_name, asset_type, relation_type, source, actor, created_at, updated_at
	`, item.ID, item.ResourceType, item.ResourceID, item.ResourceName, item.AssetID, item.AssetName, item.AssetType, item.RelationType, item.Source, item.Actor, item.CreatedAt, item.UpdatedAt)
	return scanRelation(row)
}

func (s *Service) DeleteRelation(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrRelationValidation
	}
	if s.db == nil {
		s.relationMu.Lock()
		defer s.relationMu.Unlock()
		for index, item := range s.relations {
			if item.ID == id {
				s.relations = append(s.relations[:index], s.relations[index+1:]...)
				return nil
			}
		}
		return ErrNotFound
	}
	queryContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	result, err := s.db.ExecContext(queryContext, `DELETE FROM cicd_resource_relations WHERE id = $1`, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) SuggestRelations(resourceType, resourceID, resourceName string, candidates []AssetCandidate, limit int) []RelationSuggestion {
	name := compactMatchValue(resourceName)
	id := compactMatchValue(resourceID)
	result := make([]RelationSuggestion, 0, len(candidates))
	for _, candidate := range candidates {
		assetName := compactMatchValue(candidate.Name)
		assetID := compactMatchValue(candidate.ID)
		score, reason := 0, ""
		switch {
		case id != "" && (id == assetID || strings.Contains(id, assetID) || strings.Contains(assetID, id)):
			score, reason = 100, "资源编号与资产编号匹配"
		case candidate.IP != "" && strings.Contains(resourceID, candidate.IP):
			score, reason = 98, "资源地址包含资产 IP"
		case name != "" && assetName != "" && name == assetName:
			score, reason = 92, "资源名称与资产名称完全匹配"
		case name != "" && assetName != "" && (strings.Contains(name, assetName) || strings.Contains(assetName, name)):
			score, reason = 82, "资源名称与资产名称部分匹配"
		case relationTypeCompatible(resourceType, candidate.Type):
			score, reason = 35, "资产类型可作为关联目标"
		}
		if score > 0 {
			result = append(result, RelationSuggestion{AssetID: candidate.ID, AssetName: candidate.Name, AssetType: candidate.Type, AssetIP: candidate.IP, Score: score, Reason: reason})
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Score > result[j].Score })
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

type relationScanner interface {
	Scan(dest ...any) error
}

func scanRelation(scanner relationScanner) (ResourceRelation, error) {
	var item ResourceRelation
	err := scanner.Scan(&item.ID, &item.ResourceType, &item.ResourceID, &item.ResourceName, &item.AssetID, &item.AssetName, &item.AssetType, &item.RelationType, &item.Source, &item.Actor, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func sortRelations(items []ResourceRelation) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].UpdatedAt > items[j].UpdatedAt })
}

func newRelationID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return "rel-" + time.Now().Format("20060102150405")
	}
	return "rel-" + hex.EncodeToString(buffer)
}

func compactMatchValue(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, strings.TrimSpace(value))
}

func relationTypeCompatible(resourceType, assetType string) bool {
	switch resourceType {
	case "argocd-application":
		return assetType == "k8s-cluster" || assetType == "k8s-node" || assetType == "virtual-machine" || assetType == "physical-server"
	case "harbor-repository", "gitlab-project":
		return assetType == "middleware" || assetType == "database" || assetType == "virtual-machine" || assetType == "physical-server"
	default:
		return false
	}
}
