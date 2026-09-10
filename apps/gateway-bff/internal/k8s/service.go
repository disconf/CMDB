package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
)

type AssetStore interface {
	GetAsset(string) (cmdb.Asset, error)
	FindHostByIP(string, string) string
	CreateAsset(cmdb.CreateAssetInput, string) (cmdb.Asset, error)
	UpdateAsset(string, cmdb.UpdateAssetInput, string) (cmdb.Asset, error)
}

type Status struct {
	Configured  bool   `json:"configured"`
	Ready       bool   `json:"ready"`
	ClusterName string `json:"clusterName,omitempty"`
	APIServer   string `json:"apiServer,omitempty"`
	Version     string `json:"version,omitempty"`
	LastSync    string `json:"lastSync,omitempty"`
	Message     string `json:"message,omitempty"`
}
type SyncResult struct {
	StartedAt  string         `json:"startedAt"`
	FinishedAt string         `json:"finishedAt"`
	Total      int            `json:"total"`
	Created    int            `json:"created"`
	Updated    int            `json:"updated"`
	Unchanged  int            `json:"unchanged"`
	Failed     int            `json:"failed"`
	Counts     map[string]int `json:"counts"`
	Errors     []string       `json:"errors"`
}
type Service struct {
	mu         sync.RWMutex
	client     *Client
	store      AssetStore
	lastStatus Status
	lastResult SyncResult
}

func NewService(store AssetStore) *Service {
	s := &Service{store: store}
	client, err := NewClientFromEnv()
	if err != nil {
		s.lastStatus = Status{Configured: false, Ready: false, Message: err.Error()}
		return s
	}
	s.client = client
	s.lastStatus = Status{Configured: true, Ready: true, ClusterName: client.ClusterName(), APIServer: client.BaseURL()}
	return s
}

func (s *Service) Status(ctx context.Context) Status {
	s.mu.RLock()
	status := s.lastStatus
	s.mu.RUnlock()
	if s.client == nil {
		return status
	}
	var version Version
	if err := s.client.get(ctx, "/version", &version); err != nil {
		status.Ready = false
		status.Message = err.Error()
		s.mu.Lock()
		s.lastStatus = status
		s.mu.Unlock()
		return status
	}
	status.Ready = true
	status.Version = version.GitVersion
	status.Message = ""
	s.mu.Lock()
	s.lastStatus = status
	s.mu.Unlock()
	return status
}

func (s *Service) LastResult() SyncResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := s.lastResult
	result.Counts = copyCounts(result.Counts)
	result.Errors = append([]string(nil), result.Errors...)
	return result
}

func (s *Service) Sync(ctx context.Context, operator string) (SyncResult, error) {
	if s.client == nil || s.store == nil {
		return SyncResult{}, fmt.Errorf("Kubernetes collector is not configured")
	}
	started := time.Now()
	result := SyncResult{StartedAt: started.Format("2006-01-02 15:04:05"), Counts: map[string]int{}, Errors: []string{}}
	snapshot, err := s.client.Collect(ctx)
	if err != nil {
		return result, err
	}
	resources := BuildResources(s.client.ClusterName(), s.client.BaseURL(), snapshot)
	result.Total = len(resources)
	idMap := map[string]string{}
	for _, resource := range resources {
		asset, status, err := s.upsert(resource.Asset, operator)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", resource.Asset.ID, err))
			continue
		}
		idMap[resource.Key] = asset.ID
		switch status {
		case "created":
			result.Created++
		case "updated":
			result.Updated++
		default:
			result.Unchanged++
		}
		result.Counts[resource.Asset.Type]++
	}
	for _, resource := range resources {
		assetID := idMap[resource.Key]
		if assetID == "" || len(resource.Relations) == 0 {
			continue
		}
		relations := []cmdb.Relation{}
		for _, relation := range resource.Relations {
			if targetID := idMap[relation.TargetKey]; targetID != "" {
				relations = append(relations, cmdb.Relation{Type: relation.Type, TargetID: targetID, TargetName: targetID})
			}
		}
		if len(relations) == 0 {
			continue
		}
		existing, err := s.store.GetAsset(assetID)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s relations: %v", assetID, err))
			continue
		}
		mergedRelations := mergeRelations(existing.Relations, relations)
		_, err = s.store.UpdateAsset(assetID, cmdb.UpdateAssetInput{Name: existing.Name, Status: existing.Status, IP: existing.IP, Environment: existing.Environment, ProjectGroup: existing.ProjectGroup, Owner: existing.Owner, Location: existing.Location, Tags: existing.Tags, Attributes: existing.Attributes, Relations: mergedRelations}, operator)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s relations: %v", assetID, err))
		}
	}
	result.FinishedAt = time.Now().Format("2006-01-02 15:04:05")
	status := s.Status(ctx)
	status.LastSync = result.FinishedAt
	s.mu.Lock()
	s.lastResult = result
	s.lastStatus = status
	s.mu.Unlock()
	return result, nil
}

func (s *Service) upsert(input cmdb.CreateAssetInput, operator string) (cmdb.Asset, string, error) {
	existing, err := s.store.GetAsset(input.ID)
	if err != nil && input.Type == "k8s-node" && input.IP != "" {
		if canonical := s.store.FindHostByIP(input.IP, input.ID); canonical != "" {
			existing, err = s.store.GetAsset(canonical)
		}
	}
	if err != nil {
		created, createErr := s.store.CreateAsset(input, operator)
		return created, "created", createErr
	}
	merged := cmdb.CreateAssetInput{ID: existing.ID, Name: input.Name, Type: existing.Type, Status: input.Status, IP: input.IP, Environment: existing.Environment, ProjectGroup: existing.ProjectGroup, Owner: existing.Owner, Location: input.Location, Source: existing.Source, Tags: mergeTags(existing.Tags, input.Tags), Attributes: mergeAttributes(existing.Attributes, input.Attributes)}
	if merged.Name == "" {
		merged.Name = existing.Name
	}
	if merged.Status == "" {
		merged.Status = existing.Status
	}
	if merged.IP == "" {
		merged.IP = existing.IP
	}
	if merged.Environment == "" {
		merged.Environment = "待确认"
	}
	if merged.ProjectGroup == "" {
		merged.ProjectGroup = "Kubernetes"
	}
	if merged.Owner == "" {
		merged.Owner = "待分配"
	}
	updated, updateErr := s.store.UpdateAsset(existing.ID, cmdb.UpdateAssetInput{Name: merged.Name, Status: merged.Status, IP: merged.IP, Environment: merged.Environment, ProjectGroup: merged.ProjectGroup, Owner: merged.Owner, Location: merged.Location, Tags: merged.Tags, Attributes: merged.Attributes}, operator)
	if updateErr != nil {
		return cmdb.Asset{}, "", updateErr
	}
	changed := len(diffChanges(existing, updated)) > 0
	if changed {
		return updated, "updated", nil
	}
	return updated, "unchanged", nil
}

func mergeAttributes(existing, incoming []cmdb.Attribute) []cmdb.Attribute {
	out := append([]cmdb.Attribute(nil), existing...)
	for _, item := range incoming {
		found := false
		for i := range out {
			if out[i].Name == item.Name {
				out[i] = item
				found = true
				break
			}
		}
		if !found {
			out = append(out, item)
		}
	}
	return out
}
func mergeTags(existing, incoming []string) []string {
	out := append([]string(nil), existing...)
	seen := map[string]bool{}
	for _, tag := range out {
		seen[tag] = true
	}
	for _, tag := range incoming {
		if tag != "" && !seen[tag] {
			seen[tag] = true
			out = append(out, tag)
		}
	}
	sort.Strings(out)
	return out
}
func mergeRelations(existing, incoming []cmdb.Relation) []cmdb.Relation {
	out := append([]cmdb.Relation(nil), existing...)
	seen := map[string]bool{}
	for _, item := range out {
		seen[item.Type+"|"+item.TargetID] = true
	}
	for _, item := range incoming {
		if !seen[item.Type+"|"+item.TargetID] {
			out = append(out, item)
			seen[item.Type+"|"+item.TargetID] = true
		}
	}
	return out
}
func copyCounts(values map[string]int) map[string]int {
	out := map[string]int{}
	for key, value := range values {
		out[key] = value
	}
	return out
}
func diffChanges(before, after cmdb.Asset) []string {
	changes := []string{}
	if before.Name != after.Name {
		changes = append(changes, "name")
	}
	if before.Status != after.Status {
		changes = append(changes, "status")
	}
	if before.IP != after.IP {
		changes = append(changes, "ip")
	}
	if strings.Join(attrPairs(before.Attributes), "|") != strings.Join(attrPairs(after.Attributes), "|") {
		changes = append(changes, "attributes")
	}
	return changes
}
func attrPairs(items []cmdb.Attribute) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Name+"="+item.Value)
	}
	sort.Strings(out)
	return out
}
