package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cicd"
	"cmdb/gateway-bff/internal/cmdb"
)

type argoApplicationView struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Project      string `json:"project"`
	Destination  string `json:"destination"`
	SyncStatus   string `json:"syncStatus"`
	HealthStatus string `json:"healthStatus"`
	Revision     string `json:"revision"`
	AssetID      string `json:"assetId"`
	Onboarded    bool   `json:"onboarded"`
}

type argoSyncResult struct {
	Total   int `json:"total"`
	Created int `json:"created"`
	Updated int `json:"updated"`
	Linked  int `json:"linked"`
}

func registerArgoCDOnboardingRoutes(mux *http.ServeMux, authService *auth.Service, auditService *audit.Service, cicdService *cicd.Service, cmdbService *cmdb.Service) {
	mux.HandleFunc("GET /api/v1/cicd/argocd/applications", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:view") {
			return
		}
		overview := cicdService.Resources(r.Context(), false)
		result := make([]argoApplicationView, 0, len(overview.ArgoCD.Items))
		for _, app := range overview.ArgoCD.Items {
			assetID := argoApplicationAssetID(app.Namespace, app.Name)
			_, err := cmdbService.GetAsset(assetID)
			result = append(result, argoApplicationView{Name: app.Name, Namespace: app.Namespace, Project: app.Project, Destination: app.Destination, SyncStatus: app.SyncStatus, HealthStatus: app.HealthStatus, Revision: app.Revision, AssetID: assetID, Onboarded: err == nil})
		}
		writeJSON(w, http.StatusOK, map[string]any{"available": overview.ArgoCD.Available, "applications": result})
	})

	mux.HandleFunc("POST /api/v1/cicd/argocd/applications/sync", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := assetScopeForUser(user)
		assets := cmdbService.InventoryAssetsFor(scope)
		result := syncArgoApplications(r.Context(), cicdService, cmdbService, assets, user.Username)
		auditService.Record(user.Username, "cicd.argocd.applications.sync", "argocd-applications", "total="+strconv.Itoa(result.Total)+",created="+strconv.Itoa(result.Created)+",updated="+strconv.Itoa(result.Updated))
		writeJSON(w, http.StatusOK, result)
	})
}

func syncArgoApplications(ctx context.Context, cicdService *cicd.Service, cmdbService *cmdb.Service, assets []cmdb.Asset, operator string) argoSyncResult {
	overview := cicdService.Resources(ctx, true)
	result := argoSyncResult{Total: len(overview.ArgoCD.Items)}
	for _, app := range overview.ArgoCD.Items {
		assetID := argoApplicationAssetID(app.Namespace, app.Name)
		existing, err := cmdbService.GetAsset(assetID)
		created := err != nil
		input := cmdb.CreateAssetInput{ID: assetID, Name: app.Name, Type: "cicd-application", Status: argoApplicationAssetStatus(app.HealthStatus, app.SyncStatus), Environment: "容器平台", ProjectGroup: "容器平台组", Owner: "待分配", Location: "Argo CD / " + app.Namespace, Source: "argocd", Tags: []string{"ArgoCD", "GitOps", app.Namespace, app.SyncStatus}, Attributes: argocdApplicationAttributes(app), Relations: []cmdb.Relation{}}
		if err == nil {
			input.Environment, input.ProjectGroup, input.Owner, input.Location = existing.Environment, existing.ProjectGroup, existing.Owner, existing.Location
			input.Tags = append(input.Tags, existing.Tags...)
			input.Attributes = mergeAssetAttributes(existing.Attributes, input.Attributes)
			input.Relations = append([]cmdb.Relation(nil), existing.Relations...)
		}
		if cluster := matchingK8sCluster(assets, app); cluster != nil {
			input.Relations = upsertCMDBRelation(input.Relations, cmdb.Relation{Type: "belongs-to", TargetID: cluster.ID, TargetName: cluster.Name, Source: "argocd", Status: cluster.Status})
			result.Linked++
		}
		if _, err := cmdbService.UpsertExternalAsset(input, operator); err != nil {
			continue
		}
		if created {
			result.Created++
		} else {
			result.Updated++
		}
	}
	return result
}

func argoApplicationAssetID(namespace, name string) string {
	namespace = strings.TrimSpace(strings.ToLower(namespace))
	name = strings.TrimSpace(strings.ToLower(name))
	if namespace == "" {
		namespace = "argocd"
	}
	return "argocd-application-" + strings.NewReplacer("/", "-", " ", "-", "_", "-", ".", "-").Replace(namespace) + "-" + strings.NewReplacer("/", "-", " ", "-", "_", "-", ".", "-").Replace(name)
}

func argocdApplicationAttributes(app cicd.ArgoApplication) []cmdb.Attribute {
	return []cmdb.Attribute{
		{Name: "application_name", Label: "应用名称", Value: app.Name},
		{Name: "source_type", Label: "来源类型", Value: "Argo CD"},
		{Name: "project", Label: "Argo Project", Value: app.Project},
		{Name: "namespace", Label: "目标命名空间", Value: app.Namespace},
		{Name: "destination", Label: "目标集群", Value: app.Destination},
		{Name: "sync_status", Label: "同步状态", Value: app.SyncStatus},
		{Name: "health_status", Label: "健康状态", Value: app.HealthStatus},
		{Name: "revision", Label: "当前版本", Value: app.Revision},
		{Name: "last_operation", Label: "最近操作", Value: app.LastOperation},
		{Name: "updated_at", Label: "同步时间", Value: app.UpdatedAt},
	}
}

func matchingK8sCluster(assets []cmdb.Asset, app cicd.ArgoApplication) *cmdb.Asset {
	var clusters []cmdb.Asset
	for _, asset := range assets {
		if asset.Type == "k8s-cluster" {
			clusters = append(clusters, asset)
		}
	}
	haystack := strings.ToLower(app.Name + " " + app.Project + " " + app.Destination)
	for index := range clusters {
		candidate := strings.ToLower(clusters[index].Name + " " + clusters[index].ID + " " + cmdbAttributeValue(clusters[index].Attributes, "api_server"))
		if candidate != "" && (strings.Contains(haystack, candidate) || strings.Contains(candidate, strings.ToLower(app.Name))) {
			return &clusters[index]
		}
	}
	if len(clusters) == 1 {
		return &clusters[0]
	}
	return nil
}

func cmdbAttributeValue(attributes []cmdb.Attribute, name string) string {
	for _, attribute := range attributes {
		if attribute.Name == name {
			return attribute.Value
		}
	}
	return ""
}

func mergeAssetAttributes(existing, incoming []cmdb.Attribute) []cmdb.Attribute {
	byName := make(map[string]cmdb.Attribute, len(existing)+len(incoming))
	for _, attribute := range existing {
		byName[attribute.Name] = attribute
	}
	for _, attribute := range incoming {
		byName[attribute.Name] = attribute
	}
	result := make([]cmdb.Attribute, 0, len(byName))
	for _, attribute := range byName {
		result = append(result, attribute)
	}
	return result
}

func upsertCMDBRelation(relations []cmdb.Relation, next cmdb.Relation) []cmdb.Relation {
	for index := range relations {
		if relations[index].Type == next.Type && relations[index].TargetID == next.TargetID {
			relations[index] = next
			return relations
		}
	}
	return append(relations, next)
}

func argoApplicationAssetStatus(health, sync string) string {
	if strings.EqualFold(health, "Degraded") || strings.EqualFold(sync, "OutOfSync") {
		return "warning"
	}
	if strings.EqualFold(health, "Healthy") && strings.EqualFold(sync, "Synced") {
		return "online"
	}
	return "unknown"
}
