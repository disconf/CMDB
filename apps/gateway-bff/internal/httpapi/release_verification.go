package httpapi

import (
	"context"
	"net/http"
	"strings"

	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cicd"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/releases"
)

func registerReleaseVerificationRoutes(mux *http.ServeMux, authService *auth.Service, releaseService *releases.Service, cmdbService *cmdb.Service, monitorService *monitor.Service, cicdService *cicd.Service) {
	mux.HandleFunc("GET /api/v1/releases/{id}/verify", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:view") {
			return
		}
		writeReleaseVerification(w, r, authService, releaseService, cmdbService, monitorService, cicdService, false)
	})
	mux.HandleFunc("POST /api/v1/releases/{id}/verify", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:manage") {
			return
		}
		writeReleaseVerification(w, r, authService, releaseService, cmdbService, monitorService, cicdService, true)
	})
}

func writeReleaseVerification(w http.ResponseWriter, r *http.Request, authService *auth.Service, releaseService *releases.Service, cmdbService *cmdb.Service, monitorService *monitor.Service, cicdService *cicd.Service, allowAutoRollback bool) {
	user, _ := authService.CurrentUser(bearerToken(r))
	item, err := releaseService.Get(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "RELEASE_NOT_FOUND"})
		return
	}
	result := buildReleaseVerification(r.Context(), item, user, cmdbService, monitorService, cicdService)
	rollbackReleaseID := ""
	if allowAutoRollback {
		var input struct {
			AutoRollback bool `json:"autoRollback"`
		}
		_ = decodeJSON(r, &input)
		if input.AutoRollback && result.CanAutoRollback {
			environment := item.Environment
			if environment == "" {
				environment = "unknown"
			}
			if rollback, rollbackErr := releaseService.AutoRequestRollback(item.Application, environment, item.Version, result.Reason); rollbackErr == nil {
				rollbackReleaseID = rollback.ID
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"verification": result, "rollbackReleaseId": rollbackReleaseID})
}

func buildReleaseVerification(ctx context.Context, release releases.Release, user auth.User, cmdbService *cmdb.Service, monitorService *monitor.Service, cicdService *cicd.Service) releases.VerificationResult {
	scope := assetScopeForUser(user)
	application := strings.ToLower(strings.TrimSpace(release.Application))
	relations := cicdService.ListRelations(ctx, "")
	relatedIDs := make(map[string]bool)
	for _, relation := range relations {
		if strings.Contains(strings.ToLower(relation.ResourceName+" "+relation.ResourceID), application) {
			relatedIDs[relation.AssetID] = true
		}
	}
	assets := cmdbService.InventoryAssetsFor(scope)
	assetIDs := make([]string, 0)
	for _, asset := range assets {
		if relatedIDs[asset.ID] || strings.Contains(strings.ToLower(asset.Name+" "+asset.ID), application) {
			assetIDs = append(assetIDs, asset.ID)
		}
	}
	signals := monitorService.DeploymentSignals(assetIDs)
	criticalAlerts, warningAlerts := 0, 0
	for _, alert := range monitorService.Alerts() {
		if alert.Status == "resolved" || alert.Status == "silenced" {
			continue
		}
		if application != "" && !strings.Contains(strings.ToLower(alert.Target+" "+alert.Title), application) {
			continue
		}
		if strings.EqualFold(alert.Severity, "critical") {
			criticalAlerts++
		} else if strings.EqualFold(alert.Severity, "warning") {
			warningAlerts++
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
	return releases.EvaluateVerification(releases.VerificationInput{Release: release, Argo: argo, PrometheusReady: signals.Available, TargetsUp: signals.TargetsUp, TargetsDown: signals.TargetsDown, TargetsUnknown: signals.TargetsUnknown, CriticalAlerts: criticalAlerts, WarningAlerts: warningAlerts})
}
