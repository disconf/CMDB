package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/auth"
)

type auditResponseWriter struct {
	http.ResponseWriter
	status int
	body   []byte
}

func (w *auditResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *auditResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if len(w.body) < 8192 {
		remaining := 8192 - len(w.body)
		if remaining > len(data) {
			remaining = len(data)
		}
		w.body = append(w.body, data[:remaining]...)
	}
	return w.ResponseWriter.Write(data)
}

func auditTrail(next http.Handler, authService *auth.Service, auditService *audit.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor := ""
		if shouldAuditMutation(r.Method, r.URL.Path) && bearerToken(r) != "" {
			if user, err := authService.CurrentUser(bearerToken(r)); err == nil {
				actor = user.Username
			}
		}
		recorder := &auditResponseWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		if actor == "" || recorder.status < 200 || recorder.status >= 400 {
			return
		}
		action := auditAction(r.Method, r.URL.Path)
		target := auditTarget(r.URL.Path)
		if id := responseID(recorder.body); id != "" {
			target = id
		}
		detail := r.Method + " " + r.URL.Path + " -> " + strconv.Itoa(recorder.status)
		auditService.Record(actor, action, target, detail)
	})
}

func shouldAuditMutation(method, path string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	excluded := []string{
		"/api/v1/auth/login",
		"/api/v1/agent/report",
		"/api/v1/monitor/alerts/webhook",
		"/api/v1/discovery/ingest",
		"/api/v1/discovery/scan-",
		"/api/v1/discovery/pending/",
		"/api/v1/credentials",
	}
	for _, prefix := range excluded {
		if path == prefix || strings.HasPrefix(path, prefix) {
			return false
		}
	}
	return strings.HasPrefix(path, "/api/v1/")
}

func auditAction(method, path string) string {
	switch {
	case method == http.MethodPost && path == "/api/v1/cmdb/assets":
		return "cmdb.asset.create"
	case method == http.MethodPatch && strings.HasPrefix(path, "/api/v1/cmdb/assets/"):
		return "cmdb.asset.update"
	case method == http.MethodPost && path == "/api/v1/cmdb/imports":
		return "cmdb.asset.import"
	case method == http.MethodPost && path == "/api/v1/cmdb/models":
		return "cmdb.model.create"
	case method == http.MethodPut && strings.HasPrefix(path, "/api/v1/cmdb/models/"):
		return "cmdb.model.update"
	case method == http.MethodPost && strings.HasSuffix(path, "/toggle") && strings.HasPrefix(path, "/api/v1/cmdb/models/"):
		return "cmdb.model.toggle"
	case method == http.MethodPost && path == "/api/v1/discovery/remote-executions":
		return "discovery.remote_execution.create"
	case method == http.MethodPost && strings.HasSuffix(path, "/approve") && strings.HasPrefix(path, "/api/v1/discovery/remote-executions/"):
		return "discovery.remote_execution.approve"
	case method == http.MethodPost && path == "/api/v1/discovery/agent-install":
		return "discovery.agent.install"
	case method == http.MethodPost && path == "/api/v1/discovery/agent-uninstall":
		return "discovery.agent.uninstall"
	case method == http.MethodPost && path == "/api/v1/discovery/tasks":
		return "discovery.task.create"
	case method == http.MethodPost && strings.HasSuffix(path, "/run") && strings.HasPrefix(path, "/api/v1/discovery/tasks/"):
		return "discovery.task.run"
	case method == http.MethodPost && strings.HasSuffix(path, "/reconcile") && strings.HasPrefix(path, "/api/v1/discovery/tasks/"):
		return "discovery.task.reconcile"
	case method == http.MethodPost && path == "/api/v1/idc/rooms":
		return "idc.room.create"
	case method == http.MethodPost && strings.HasSuffix(path, "/modules") && strings.HasPrefix(path, "/api/v1/idc/rooms/"):
		return "idc.module.create"
	case method == http.MethodPost && path == "/api/v1/idc/racks":
		return "idc.rack.create"
	case method == http.MethodPatch && strings.HasPrefix(path, "/api/v1/idc/racks/"):
		return "idc.rack.update"
	case method == http.MethodPatch && strings.HasPrefix(path, "/api/v1/idc/modules/"):
		return "idc.module.update"
	case method == http.MethodDelete && strings.HasPrefix(path, "/api/v1/idc/racks/"):
		return "idc.rack.delete"
	case method == http.MethodDelete && strings.HasPrefix(path, "/api/v1/idc/modules/"):
		return "idc.module.delete"
	case method == http.MethodDelete && strings.HasPrefix(path, "/api/v1/idc/rooms/"):
		return "idc.room.delete"
	case method == http.MethodPost && path == "/api/v1/jobs/templates":
		return "job.template.create"
	case method == http.MethodPost && strings.HasSuffix(path, "/toggle") && strings.HasPrefix(path, "/api/v1/jobs/templates/"):
		return "job.template.toggle"
	case method == http.MethodPost && path == "/api/v1/jobs/executions":
		return "job.execution.create"
	case strings.HasPrefix(path, "/api/v1/jobs/executions/") && method == http.MethodPost:
		return "job.execution." + path[strings.LastIndex(path, "/")+1:]
	case method == http.MethodPost && path == "/api/v1/tickets":
		return "ticket.create"
	case method == http.MethodPost && strings.HasSuffix(path, "/approve") && strings.HasPrefix(path, "/api/v1/tickets/"):
		return "ticket.approve"
	case method == http.MethodPost && strings.HasSuffix(path, "/reject") && strings.HasPrefix(path, "/api/v1/tickets/"):
		return "ticket.reject"
	case method == http.MethodPost && path == "/api/v1/releases":
		return "release.create"
	case method == http.MethodPost && strings.HasSuffix(path, "/rollback") && strings.HasPrefix(path, "/api/v1/releases/"):
		return "release.rollback"
	case method == http.MethodPost && path == "/api/v1/toolbox/executions":
		return "toolbox.execution.create"
	case method == http.MethodPost && strings.HasSuffix(path, "/execute") && strings.HasPrefix(path, "/api/v1/aiops/analyses/"):
		return "aiops.analysis.execute"
	case method == http.MethodPost && strings.HasSuffix(path, "/toggle") && strings.HasPrefix(path, "/api/v1/system/users/"):
		return "system.user.toggle"
	case method == http.MethodPut && strings.HasSuffix(path, "/permissions") && strings.HasPrefix(path, "/api/v1/system/roles/"):
		return "system.role.permissions"
	case method == http.MethodPost && path == "/api/v1/monitor/alerts/bulk":
		return "monitor.alert.bulk"
	}
	return strings.ToLower(method) + ".api"
}

func auditTarget(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	last := parts[len(parts)-1]
	verbs := map[string]bool{
		"toggle": true, "run": true, "reconcile": true, "approve": true,
		"reject": true, "retry": true, "cancel": true, "rollback": true,
		"execute": true, "permissions": true,
	}
	if verbs[last] && len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	return last
}

func responseID(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var value struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(body, &value) != nil {
		return ""
	}
	return value.ID
}
