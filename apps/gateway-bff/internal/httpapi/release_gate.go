package httpapi

import (
	"context"
	"net/http"
	"os"
	"strings"

	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cicd"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/releases"
)

func registerReleaseGateRoutes(mux *http.ServeMux, authService *auth.Service, releaseService *releases.Service, cmdbService *cmdb.Service, monitorService *monitor.Service, cicdService *cicd.Service) {
	mux.HandleFunc("GET /api/v1/releases/{id}/gate-check", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := releaseService.Get(r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "RELEASE_NOT_FOUND"})
			return
		}
		writeJSON(w, http.StatusOK, evaluateReleaseGate(r.Context(), item, user, cmdbService, monitorService, cicdService))
	})
}

func evaluateReleaseGate(ctx context.Context, release releases.Release, user auth.User, cmdbService *cmdb.Service, monitorService *monitor.Service, cicdService *cicd.Service) releases.GateResult {
	if release.ApprovalType == "rollback" {
		return releases.EvaluateGate(releases.GateInput{Release: release, ChangeWindows: "*"})
	}
	alerts := make([]releases.GateAlert, 0)
	for _, alert := range monitorService.Alerts() {
		alerts = append(alerts, releases.GateAlert{ID: alert.ID, Title: alert.Title, Severity: alert.Severity, Status: alert.Status, Target: alert.Target})
	}
	scope := assetScopeForUser(user)
	assetItems := cmdbService.InventoryAssetsFor(scope)
	relations := cicdService.ListRelations(ctx, "")
	relatedIDs := make(map[string]bool)
	application := strings.ToLower(strings.TrimSpace(release.Application))
	for _, relation := range relations {
		haystack := strings.ToLower(relation.ResourceName + " " + relation.ResourceID)
		if application != "" && strings.Contains(haystack, application) {
			relatedIDs[relation.AssetID] = true
		}
	}
	assets := make([]releases.GateAsset, 0)
	for _, asset := range assetItems {
		haystack := strings.ToLower(asset.Name + " " + asset.ID + " " + asset.ProjectGroup)
		if relatedIDs[asset.ID] || (application != "" && strings.Contains(haystack, application)) {
			assets = append(assets, releases.GateAsset{ID: asset.ID, Name: asset.Name, Type: asset.Type, Status: asset.Status, Env: asset.Environment, Project: asset.ProjectGroup})
		}
	}
	var argo *releases.GateArgo
	overview := cicdService.Resources(ctx, false)
	for _, app := range overview.ArgoCD.Items {
		haystack := strings.ToLower(app.Name + " " + app.Namespace + " " + app.Project)
		if application != "" && (strings.Contains(haystack, application) || strings.Contains(application, strings.ToLower(app.Name))) {
			argo = &releases.GateArgo{Name: app.Name, SyncStatus: app.SyncStatus, HealthStatus: app.HealthStatus}
			break
		}
	}
	return releases.EvaluateGate(releases.GateInput{Release: release, ChangeWindows: os.Getenv("RELEASE_CHANGE_WINDOWS"), Alerts: alerts, Assets: assets, Argo: argo})
}
