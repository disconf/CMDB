package httpapi

import (
	"errors"
	"net/http"
	"time"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/license"
)

func registerLicenseRoutes(mux *http.ServeMux, authService *auth.Service, auditService *audit.Service, licenseService *license.Service, cmdbService *cmdb.Service) {
	mux.HandleFunc("GET /api/v1/license/status", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "system:manage") {
			return
		}
		writeJSON(w, http.StatusOK, licenseService.Status(cmdbService.AssetCount()))
	})
	mux.HandleFunc("GET /api/v1/license/request", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "system:manage") {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"product":        "CMDB 智能运维平台",
			"deploymentId":   licenseService.DeploymentID(),
			"freeAssetLimit": license.FreeAssetLimit,
			"generatedAt":    time.Now().UTC().Format(time.RFC3339),
		})
	})
	mux.HandleFunc("POST /api/v1/license/activate", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "system:manage") {
			return
		}
		var input struct {
			LicenseKey string `json:"licenseKey"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "许可证内容格式不正确"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		_, err := licenseService.Activate(input.LicenseKey, user.Username)
		if err != nil {
			switch {
			case errors.Is(err, license.ErrDeploymentMismatch):
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "LICENSE_DEPLOYMENT_MISMATCH", "message": "许可证绑定的部署实例与当前平台不一致"})
			case errors.Is(err, license.ErrExpiredLicense):
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "LICENSE_EXPIRED", "message": "许可证已过期"})
			case errors.Is(err, license.ErrInvalidSignature), errors.Is(err, license.ErrInvalidToken):
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "LICENSE_INVALID", "message": "许可证签名或内容无效"})
			default:
				writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "LICENSE_ACTIVATE_FAILED", "message": err.Error()})
			}
			return
		}
		status := licenseService.Status(cmdbService.AssetCount())
		auditService.Record(user.Username, "license.activate", status.LicenseID, status.Customer)
		writeJSON(w, http.StatusOK, status)
	})
}
