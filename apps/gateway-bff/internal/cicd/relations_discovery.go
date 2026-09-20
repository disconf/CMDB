package cicd

import (
	"context"
	"strconv"
	"strings"
)

type AutoDiscoveryInput struct {
	ResourceTypes []string `json:"resourceTypes"`
	Apply         bool     `json:"apply"`
}

type AutoDiscoveryItem struct {
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
	ResourceName string `json:"resourceName"`
	AssetID      string `json:"assetId,omitempty"`
	AssetName    string `json:"assetName,omitempty"`
	AssetType    string `json:"assetType,omitempty"`
	Score        int    `json:"score"`
	Reason       string `json:"reason,omitempty"`
	Action       string `json:"action"`
}

type AutoDiscoveryResult struct {
	Scanned int                 `json:"scanned"`
	Matched int                 `json:"matched"`
	Applied int                 `json:"applied"`
	Updated int                 `json:"updated"`
	Skipped int                 `json:"skipped"`
	Failed  int                 `json:"failed"`
	Items   []AutoDiscoveryItem `json:"items"`
}

type autoResource struct {
	ResourceType string
	ResourceID   string
	ResourceName string
	RelationType string
}

func (s *Service) AutoDiscoverRelations(ctx context.Context, resourceTypes []string, candidates []AssetCandidate, actor string, apply bool) AutoDiscoveryResult {
	allowed := make(map[string]bool)
	for _, resourceType := range resourceTypes {
		if resourceType != "" {
			allowed[resourceType] = true
		}
	}
	if len(allowed) == 0 {
		allowed["harbor-repository"] = true
		allowed["argocd-application"] = true
		allowed["gitlab-project"] = true
	}
	overview := s.Resources(ctx, false)
	resources := buildAutoResources(overview, allowed)
	result := AutoDiscoveryResult{Scanned: len(resources), Items: make([]AutoDiscoveryItem, 0)}
	for _, resource := range resources {
		suggestions := s.SuggestRelations(resource.ResourceType, resource.ResourceID, resource.ResourceName, candidates, 1)
		if len(suggestions) == 0 || suggestions[0].Score < 70 {
			result.Skipped++
			continue
		}
		best := suggestions[0]
		item := AutoDiscoveryItem{
			ResourceType: resource.ResourceType, ResourceID: resource.ResourceID, ResourceName: resource.ResourceName,
			AssetID: best.AssetID, AssetName: best.AssetName, AssetType: best.AssetType, Score: best.Score, Reason: best.Reason,
		}
		existing := s.ListRelations(ctx, resource.ResourceType)
		manual := false
		var staleAuto []ResourceRelation
		sameAuto := false
		for _, relation := range existing {
			if relation.ResourceID != resource.ResourceID {
				continue
			}
			if relation.Source == "manual" {
				manual = true
				break
			}
			if relation.AssetID == best.AssetID && relation.RelationType == resource.RelationType {
				sameAuto = true
				continue
			}
			staleAuto = append(staleAuto, relation)
		}
		if manual {
			item.Action = "manual-preserved"
			result.Skipped++
			result.Items = append(result.Items, item)
			continue
		}
		result.Matched++
		if !apply {
			item.Action = "suggested"
			result.Items = append(result.Items, item)
			continue
		}
		for _, old := range staleAuto {
			_ = s.DeleteRelation(ctx, old.ID)
		}
		saved, err := s.CreateRelation(ctx, RelationInput{
			ResourceType: resource.ResourceType, ResourceID: resource.ResourceID, ResourceName: resource.ResourceName,
			AssetID: best.AssetID, AssetName: best.AssetName, AssetType: best.AssetType,
			RelationType: resource.RelationType, Source: "auto",
		}, actor)
		if err != nil {
			item.Action = "failed"
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		if sameAuto {
			item.Action = "updated"
			result.Updated++
		} else {
			item.Action = "applied"
			result.Applied++
		}
		if saved.ID != "" {
			item.AssetID = saved.AssetID
		}
		result.Items = append(result.Items, item)
	}
	return result
}

func buildAutoResources(overview ResourceOverview, allowed map[string]bool) []autoResource {
	resources := make([]autoResource, 0)
	if allowed["harbor-repository"] && overview.Harbor.Available {
		for _, repository := range overview.Harbor.RecentRepositories {
			id := strings.Trim(repository.Project+"/"+repository.Name, "/")
			resources = append(resources, autoResource{ResourceType: "harbor-repository", ResourceID: id, ResourceName: repository.Name, RelationType: "built-from"})
		}
	}
	if allowed["argocd-application"] && overview.ArgoCD.Available {
		for _, application := range overview.ArgoCD.Items {
			id := strings.Trim(application.Namespace+"/"+application.Name, "/")
			resources = append(resources, autoResource{ResourceType: "argocd-application", ResourceID: id, ResourceName: application.Name, RelationType: "deployed-to"})
		}
	}
	if allowed["gitlab-project"] && overview.GitLab.Available {
		for _, project := range overview.GitLab.RecentProjects {
			resources = append(resources, autoResource{ResourceType: "gitlab-project", ResourceID: strconv.Itoa(project.ID), ResourceName: project.Name, RelationType: "belongs-to"})
		}
	}
	return resources
}
