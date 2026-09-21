package httpapi

import (
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/releases"
)

func registerCICDProviderEventRoutes(mux *http.ServeMux, authService *auth.Service, auditService *audit.Service, releaseService *releases.Service) {
	mux.HandleFunc("POST /api/v1/cicd/events/{source}", func(w http.ResponseWriter, r *http.Request) {
		if !authorizeCICDProviderWebhook(w, r) {
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "事件内容读取失败"})
			return
		}
		input, err := releases.ParseProviderEvent(r.PathValue("source"), raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_PROVIDER_EVENT", "message": err.Error()})
			return
		}
		if releaseID := strings.TrimSpace(r.Header.Get("X-Release-ID")); releaseID != "" {
			input.ExternalID = releaseID
		}
		if application := strings.TrimSpace(r.Header.Get("X-CI-Application")); application != "" {
			input.Application = application
		}
		if version := strings.TrimSpace(r.Header.Get("X-CI-Version")); version != "" {
			input.Version = version
		}
		if environment := strings.TrimSpace(r.Header.Get("X-CI-Environment")); environment != "" {
			input.Environment = environment
		}
		if ticketID := strings.TrimSpace(r.Header.Get("X-Ticket-ID")); ticketID != "" {
			input.TicketID = ticketID
		}
		item, err := releaseService.ApplyEvent(input)
		if errors.Is(err, releases.ErrValidation) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "RELEASE_EVENT_VALIDATION_FAILED", "message": "发布事件缺少必要字段"})
			return
		}
		if errors.Is(err, releases.ErrInvalidState) {
			writeJSON(w, http.StatusConflict, map[string]string{"code": "RELEASE_APPROVAL_REQUIRED", "message": "存在待审批的手工发布，外部事件不能绕过审批"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "RELEASE_EVENT_FAILED", "message": "发布事件处理失败"})
			return
		}
		auditService.Record(input.Source, "cicd.event.apply", item.ID, item.Application+" "+item.Version+" "+item.Status)
		writeJSON(w, http.StatusAccepted, item)
	})
}

func authorizeCICDProviderWebhook(w http.ResponseWriter, r *http.Request) bool {
	expected := strings.TrimSpace(os.Getenv("CICD_HEALTH_WEBHOOK_TOKEN"))
	if expected == "" {
		expected = strings.TrimSpace(os.Getenv("RELEASE_WEBHOOK_TOKEN"))
	}
	provided := strings.TrimSpace(r.Header.Get("X-CICD-Token"))
	if provided == "" {
		provided = strings.TrimSpace(r.Header.Get("X-Release-Token"))
	}
	if provided == "" {
		provided = bearerToken(r)
	}
	if expected == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED", "message": "CI/CD 事件令牌无效"})
		return false
	}
	return true
}
