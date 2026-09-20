package httpapi

import (
	"errors"
	"net/http"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cicd"
)

func registerCICDRoutes(mux *http.ServeMux, authService *auth.Service, auditService *audit.Service, service *cicd.Service) {
	mux.HandleFunc("GET /api/v1/cicd/integrations", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:view") {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, service.List())
	})

	mux.HandleFunc("POST /api/v1/cicd/integrations/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:manage") {
			return
		}
		item, err := service.Test(r.Context(), r.PathValue("id"))
		if errors.Is(err, cicd.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "CICD_INTEGRATION_NOT_FOUND", "message": "集成连接器不存在"})
			return
		}
		if errors.Is(err, cicd.ErrNotConfigured) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "CICD_INTEGRATION_NOT_CONFIGURED", "message": item.Message})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		if user.Username != "" {
			detail := item.Status
			if item.Message != "" {
				detail += ": " + item.Message
			}
			auditService.Record(user.Username, "cicd.integration.test", item.ID, detail)
		}
		writeJSON(w, http.StatusOK, item)
	})
}
