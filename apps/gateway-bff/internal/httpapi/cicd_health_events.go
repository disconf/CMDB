package httpapi

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/releases"
	"cmdb/gateway-bff/internal/tickets"
)

func registerCICDHealthEventRoutes(mux *http.ServeMux, auditService *audit.Service, monitorService *monitor.Service, ticketService *tickets.Service, cmdbService *cmdb.Service, releaseService *releases.Service) {
	mux.HandleFunc("POST /api/v1/cicd/health-events", func(w http.ResponseWriter, r *http.Request) {
		if !authorizeCICDHealthWebhook(w, r) {
			return
		}
		var input monitor.HealthEventInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "健康事件格式不正确"})
			return
		}
		fingerprint, payload := monitor.BuildHealthEventPayload(input)
		accepted, err := monitorService.ReceiveWebhook(payload)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "HEALTH_EVENT_PERSIST_FAILED", "message": "健康事件写入失败"})
			return
		}
		syncAssetHealthFromAlerts(cmdbService, monitorService, payload)
		syncTicketsFromAlerts(ticketService, auditService, payload)
		rollbackReleaseID := ""
		if payload.Status == "firing" && input.AutoRollback && strings.TrimSpace(input.Application) != "" {
			environment := strings.TrimSpace(input.Environment)
			if environment == "" {
				environment = "unknown"
			}
			if rollback, err := releaseService.AutoRequestRollback(strings.TrimSpace(input.Application), environment, strings.TrimSpace(input.Version), "CI/CD 健康事件触发自动回滚："+input.Summary); err == nil {
				rollbackReleaseID = rollback.ID
				auditService.Record("cicd-health", "release.rollback.auto_request", rollback.ID, input.Application+" "+input.Version)
			}
		}
		auditService.Record("cicd-health", "cicd.health.event", fingerprint, payload.Status+":"+input.ResourceType+"/"+input.ResourceID)
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": accepted, "fingerprint": fingerprint, "status": payload.Status, "rollbackReleaseId": rollbackReleaseID})
	})
}

func authorizeCICDHealthWebhook(w http.ResponseWriter, r *http.Request) bool {
	expected := strings.TrimSpace(os.Getenv("CICD_HEALTH_WEBHOOK_TOKEN"))
	if expected == "" {
		expected = strings.TrimSpace(os.Getenv("RELEASE_WEBHOOK_TOKEN"))
	}
	provided := strings.TrimSpace(r.Header.Get("X-CICD-Token"))
	if provided == "" {
		provided = bearerToken(r)
	}
	if expected == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED", "message": "CI/CD 健康事件令牌无效"})
		return false
	}
	return true
}
