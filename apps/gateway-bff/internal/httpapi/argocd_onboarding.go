package httpapi

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cicd"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/releases"
	"cmdb/gateway-bff/internal/tickets"
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
	Total             int      `json:"total"`
	Created           int      `json:"created"`
	Updated           int      `json:"updated"`
	Linked            int      `json:"linked"`
	Relations         int      `json:"relations"`
	Releases          int      `json:"releases"`
	ReleaseIDs        []string `json:"releaseIds,omitempty"`
	Drifted           int      `json:"drifted"`
	Remediated        int      `json:"remediated"`
	RemediationFailed int      `json:"remediationFailed"`
}

type argoSyncStatus struct {
	Running           bool     `json:"running"`
	LastRunAt         string   `json:"lastRunAt,omitempty"`
	LastSuccessAt     string   `json:"lastSuccessAt,omitempty"`
	Status            string   `json:"status"`
	Message           string   `json:"message"`
	Total             int      `json:"total"`
	Created           int      `json:"created"`
	Updated           int      `json:"updated"`
	Linked            int      `json:"linked"`
	Relations         int      `json:"relations"`
	Releases          int      `json:"releases"`
	ReleaseIDs        []string `json:"releaseIds,omitempty"`
	Drifted           int      `json:"drifted"`
	Remediated        int      `json:"remediated"`
	RemediationFailed int      `json:"remediationFailed"`
	DurationMS        int64    `json:"durationMs"`
}

type argoSyncCoordinator struct {
	mu             sync.Mutex
	status         argoSyncStatus
	cicdService    *cicd.Service
	cmdbService    *cmdb.Service
	releaseService *releases.Service
	monitorService *monitor.Service
	ticketService  *tickets.Service
	auditService   *audit.Service
	drifted        map[string]bool
}

func newArgoSyncCoordinator(cicdService *cicd.Service, cmdbService *cmdb.Service, releaseService *releases.Service, monitorService *monitor.Service, ticketService *tickets.Service, auditService *audit.Service) *argoSyncCoordinator {
	return &argoSyncCoordinator{status: argoSyncStatus{Status: "idle", Message: "等待首次同步"}, cicdService: cicdService, cmdbService: cmdbService, releaseService: releaseService, monitorService: monitorService, ticketService: ticketService, auditService: auditService, drifted: make(map[string]bool)}
}

func (c *argoSyncCoordinator) Status() argoSyncStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *argoSyncCoordinator) Sync(ctx context.Context, operator string) argoSyncResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	started := time.Now()
	c.status.Running = true
	c.status.Status = "running"
	c.status.Message = "正在同步 Argo CD Application"
	var result argoSyncResult
	acquired, lockErr := c.cicdService.WithAdvisoryLock(ctx, 741852963, func() error {
		result = syncArgoApplications(ctx, c.cicdService, c.cmdbService, c.releaseService, c.cmdbService.InventoryAssets(), operator)
		c.detectDrift(ctx, &result)
		return nil
	})
	now := time.Now().Format(time.RFC3339)
	if lockErr != nil {
		c.status = argoSyncStatus{Running: false, LastRunAt: now, Status: "error", Message: lockErr.Error(), DurationMS: time.Since(started).Milliseconds()}
		return result
	}
	if !acquired {
		c.status = argoSyncStatus{Running: false, LastRunAt: now, Status: "skipped", Message: "其他 Gateway 副本正在执行 Argo CD 同步", DurationMS: time.Since(started).Milliseconds()}
		return result
	}
	c.status = argoSyncStatus{Running: false, LastRunAt: now, LastSuccessAt: now, Status: "success", Message: "Argo CD Application 同步完成", Total: result.Total, Created: result.Created, Updated: result.Updated, Linked: result.Linked, Relations: result.Relations, Releases: result.Releases, ReleaseIDs: result.ReleaseIDs, Drifted: result.Drifted, Remediated: result.Remediated, RemediationFailed: result.RemediationFailed, DurationMS: time.Since(started).Milliseconds()}
	return result
}

func (c *argoSyncCoordinator) Run(ctx context.Context, interval time.Duration) {
	if interval < time.Minute {
		interval = 10 * time.Minute
	}
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			c.Sync(ctx, "argocd-sync")
			timer.Reset(interval)
		}
	}
}

func registerArgoCDOnboardingRoutes(mux *http.ServeMux, authService *auth.Service, auditService *audit.Service, coordinator *argoSyncCoordinator) {
	mux.HandleFunc("GET /api/v1/cicd/argocd/applications", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:view") {
			return
		}
		overview := coordinator.cicdService.Resources(r.Context(), false)
		result := make([]argoApplicationView, 0, len(overview.ArgoCD.Items))
		for _, app := range overview.ArgoCD.Items {
			assetID := argoApplicationAssetID(app.Namespace, app.Name)
			_, err := coordinator.cmdbService.GetAsset(assetID)
			result = append(result, argoApplicationView{Name: app.Name, Namespace: app.Namespace, Project: app.Project, Destination: app.Destination, SyncStatus: app.SyncStatus, HealthStatus: app.HealthStatus, Revision: app.Revision, AssetID: assetID, Onboarded: err == nil})
		}
		writeJSON(w, http.StatusOK, map[string]any{"available": overview.ArgoCD.Available, "applications": result})
	})

	mux.HandleFunc("POST /api/v1/cicd/argocd/applications/sync", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		result := coordinator.Sync(r.Context(), user.Username)
		auditService.Record(user.Username, "cicd.argocd.applications.sync", "argocd-applications", "total="+strconv.Itoa(result.Total)+",created="+strconv.Itoa(result.Created)+",updated="+strconv.Itoa(result.Updated))
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/cicd/argocd/applications/{name}/sync", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:manage") {
			return
		}
		var input struct {
			Namespace string `json:"namespace"`
			Prune     bool   `json:"prune"`
			DryRun    bool   `json:"dryRun"`
		}
		_ = decodeJSON(r, &input)
		if err := coordinator.cicdService.SyncArgoApplication(r.Context(), input.Namespace, r.PathValue("name"), input.Prune, input.DryRun); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "ARGOCD_SYNC_FAILED", "message": err.Error()})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "cicd.argocd.application.sync", r.PathValue("name"), "prune="+strconv.FormatBool(input.Prune))
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "submitted", "name": r.PathValue("name")})
	})
	mux.HandleFunc("GET /api/v1/cicd/argocd/sync/status", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:view") {
			return
		}
		writeJSON(w, http.StatusOK, coordinator.Status())
	})
}

func syncArgoApplications(ctx context.Context, cicdService *cicd.Service, cmdbService *cmdb.Service, releaseService *releases.Service, assets []cmdb.Asset, operator string) argoSyncResult {
	overview := cicdService.Resources(ctx, true)
	result := argoSyncResult{Total: len(overview.ArgoCD.Items)}
	releasesByApplication := make(map[string][]string)
	if releaseService != nil {
		if items, err := releaseService.List(); err == nil {
			for _, item := range items {
				releasesByApplication[item.Application] = append(releasesByApplication[item.Application], item.ID)
			}
		}
	}
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
		if _, err := cicdService.CreateRelation(ctx, cicd.RelationInput{ResourceType: "argocd-application", ResourceID: strings.Trim(app.Namespace+"/"+app.Name, "/"), ResourceName: app.Name, AssetID: assetID, AssetName: app.Name, AssetType: "cicd-application", RelationType: "deployed-to", Source: "auto"}, operator); err == nil {
			result.Relations++
		}
		for _, releaseID := range releasesByApplication[app.Name] {
			if !containsStringValue(result.ReleaseIDs, releaseID) {
				result.ReleaseIDs = append(result.ReleaseIDs, releaseID)
			}
		}
		if created {
			result.Created++
		} else {
			result.Updated++
		}
	}
	result.Releases = len(result.ReleaseIDs)
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

func containsStringValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func argoSyncInterval() time.Duration {
	value := strings.TrimSpace(os.Getenv("CICD_ARGOCD_SYNC_INTERVAL"))
	if value == "" {
		return 10 * time.Minute
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < time.Minute {
		return 10 * time.Minute
	}
	return duration
}

func (c *argoSyncCoordinator) detectDrift(ctx context.Context, result *argoSyncResult) {
	overview := c.cicdService.Resources(ctx, false)
	observed := make(map[string]bool)
	for _, app := range overview.ArgoCD.Items {
		key := strings.Trim(app.Namespace+"/"+app.Name, "/")
		observed[key] = true
		if strings.EqualFold(app.SyncStatus, "Synced") {
			if c.drifted[key] {
				c.resolveDrift(ctx, app)
			}
			continue
		}
		result.Drifted++
		first := !c.drifted[key]
		c.drifted[key] = true
		c.raiseDrift(ctx, app)
		if first && argoAutoSyncEnabled(app) {
			if err := c.cicdService.SyncArgoApplication(ctx, app.Namespace, app.Name, app.Prune, false); err != nil {
				result.RemediationFailed++
				c.auditService.Record("argocd-sync", "cicd.argocd.drift.remediation_failed", key, err.Error())
			} else {
				result.Remediated++
				c.auditService.Record("argocd-sync", "cicd.argocd.drift.remediated", key, "Sync 请求已提交")
			}
		}
	}
	for key := range c.drifted {
		if !observed[key] {
			c.resolveDrift(ctx, cicd.ArgoApplication{Name: key, Namespace: ""})
		}
	}
}

func (c *argoSyncCoordinator) raiseDrift(ctx context.Context, app cicd.ArgoApplication) {
	key := strings.Trim(app.Namespace+"/"+app.Name, "/")
	severity := "warning"
	if strings.EqualFold(app.HealthStatus, "Degraded") {
		severity = "critical"
	}
	input := monitor.HealthEventInput{Source: "argocd", ResourceType: "argocd-application", ResourceID: key, ResourceName: app.Name, Status: "firing", Severity: severity, Summary: "Argo CD 应用发生配置漂移", Description: "应用 " + app.Name + " 当前同步状态为 " + app.SyncStatus + "，健康状态为 " + app.HealthStatus, AssetID: argoApplicationAssetID(app.Namespace, app.Name), Application: app.Name, Environment: app.Namespace}
	_, payload := monitor.BuildHealthEventPayload(input)
	if _, err := c.monitorService.ReceiveWebhook(payload); err != nil {
		return
	}
	syncAssetHealthFromAlerts(c.cmdbService, c.monitorService, payload)
	syncTicketsFromAlerts(c.ticketService, c.auditService, payload)
}

func (c *argoSyncCoordinator) resolveDrift(ctx context.Context, app cicd.ArgoApplication) {
	key := strings.Trim(app.Namespace+"/"+app.Name, "/")
	input := monitor.HealthEventInput{Source: "argocd", ResourceType: "argocd-application", ResourceID: key, ResourceName: app.Name, Status: "resolved", Severity: "info", Summary: "Argo CD 应用漂移已恢复", Description: "应用 " + app.Name + " 已恢复同步", AssetID: argoApplicationAssetID(app.Namespace, app.Name), Application: app.Name, Environment: app.Namespace}
	_, payload := monitor.BuildHealthEventPayload(input)
	if _, err := c.monitorService.ReceiveWebhook(payload); err != nil {
		return
	}
	syncAssetHealthFromAlerts(c.cmdbService, c.monitorService, payload)
	syncTicketsFromAlerts(c.ticketService, c.auditService, payload)
	delete(c.drifted, key)
}

func argoAutoSyncEnabled(app cicd.ArgoApplication) bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("CICD_ARGOCD_AUTO_SYNC")), "true") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(app.Annotations["cmdb.jzq.com/auto-sync"]), "true")
}
