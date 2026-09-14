package httpapi

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/discovery"

	"github.com/gorilla/websocket"
)

type terminalSocketMessage struct {
	Type     string                      `json:"type"`
	Data     string                      `json:"data,omitempty"`
	Message  string                      `json:"message,omitempty"`
	Access   string                      `json:"access,omitempty"`
	Cols     int                         `json:"cols,omitempty"`
	Rows     int                         `json:"rows,omitempty"`
	Approval *discovery.TerminalApproval `json:"approval,omitempty"`
}

type terminalInputGuard struct {
	buffer     []rune
	skipEscape bool
}

func (g *terminalInputGuard) process(data string, requireApproval bool) (forward string, command string, approvalCommand string, blocked string) {
	var out strings.Builder
	for _, r := range data {
		if g.skipEscape {
			if (r >= 'A' && r <= 'Z') || r == '~' {
				g.skipEscape = false
			}
			continue
		}
		switch r {
		case 27:
			g.skipEscape = true
			continue
		case 3:
			g.buffer = g.buffer[:0]
			out.WriteRune(r)
			continue
		case 21:
			g.buffer = g.buffer[:0]
			out.WriteRune(r)
			continue
		case 127, 8:
			if len(g.buffer) > 0 {
				g.buffer = g.buffer[:len(g.buffer)-1]
			}
			out.WriteRune(r)
			continue
		case '\r', '\n':
			line := strings.TrimSpace(string(g.buffer))
			g.buffer = g.buffer[:0]
			reason := discovery.BlockedTerminalCommand(line)
			if line != "" && (reason != "" || requireApproval) {
				out.WriteByte(3)
				approvalCommand = line
				blocked = reason
				if blocked == "" {
					blocked = "安全策略要求所有交互命令先审批"
				}
				continue
			}
			out.WriteRune(r)
			command = line
			continue
		}
		if r >= 32 {
			g.buffer = append(g.buffer, r)
		}
		out.WriteRune(r)
	}
	return out.String(), command, approvalCommand, blocked
}

func registerRemoteTerminalRoutes(mux *http.ServeMux, authService *auth.Service, auditService *audit.Service, discoveryService *discovery.Service) {
	mux.HandleFunc("POST /api/v1/discovery/remote-sessions/{id}/terminal-ticket", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		ticket, err := discoveryService.CreateTerminalTicket(r.PathValue("id"), user.Username, user.Roles)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		auditService.Record(user.Username, "remote.session.terminal_ticket", r.PathValue("id"), "一次性终端票据已签发")
		writeJSON(w, http.StatusOK, ticket)
	})
	mux.HandleFunc("GET /api/v1/discovery/remote-sessions/{id}/terminal-recording", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		recording, err := discoveryService.TerminalRecording(r.PathValue("id"), user.Username, user.Roles)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "remote terminal recording not found"})
			return
		}
		writeJSON(w, http.StatusOK, recording)
	})
	mux.HandleFunc("POST /api/v1/discovery/remote-sessions/{id}/archive", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		if _, err := discoveryService.RemoteSessionReplay(r.PathValue("id"), user.Username, user.Roles); err != nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"message": "remote session is not accessible"})
			return
		}
		if err := discoveryService.ArchiveRemoteSessionRecording(r.Context(), r.PathValue("id")); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		auditService.Record(user.Username, "remote.recording.archive", r.PathValue("id"), "terminal recording archived to object storage")
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("GET /api/v1/discovery/remote-sessions/{id}/terminal", func(w http.ResponseWriter, r *http.Request) {
		item, err := discoveryService.ConsumeTerminalTicket(r.URL.Query().Get("ticket"))
		if err != nil || item.ID != r.PathValue("id") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "terminal ticket is invalid or expired"})
			return
		}

		cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
		rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
		upgrader := websocket.Upgrader{ReadBufferSize: 8192, WriteBufferSize: 8192, CheckOrigin: func(_ *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		channel, subscriber, access, err := discoveryService.OpenRemoteTerminal(item.ID, item.Operator, item.Roles, cols, rows)
		if err != nil {
			_ = conn.WriteJSON(terminalSocketMessage{Type: "error", Message: err.Error()})
			return
		}
		defer func() {
			if channel.Unsubscribe(subscriber) == 0 {
				if channel.IsOwner() {
					discoveryService.MaybeCloseRemoteTerminal(item.ID)
				} else {
					_ = channel.Close()
				}
			}
		}()
		auditService.Record(item.Operator, "remote.session.terminal_open", item.ID, item.AssetName+"/"+item.IP+" | "+access)
		discoveryService.RecordRemoteSessionEvent(item.ID, "success", "terminal-open", "交互式终端已连接")
		defer discoveryService.RecordRemoteSessionEvent(item.ID, "success", "terminal-close", "交互式终端已断开")

		var writeMu sync.Mutex
		writeMessage := func(message terminalSocketMessage) error {
			writeMu.Lock()
			defer writeMu.Unlock()
			return conn.WriteJSON(message)
		}
		if err = writeMessage(terminalSocketMessage{Type: "ready", Access: access, Message: "terminal ready"}); err != nil {
			return
		}
		output, ok := channel.Output(subscriber)
		if !ok {
			_ = writeMessage(terminalSocketMessage{Type: "error", Message: "terminal subscription unavailable"})
			return
		}
		done := make(chan struct{})
		var doneOnce sync.Once
		closeDone := func() { doneOnce.Do(func() { close(done) }) }
		go func() {
			defer closeDone()
			for chunk := range output {
				if writeErr := writeMessage(terminalSocketMessage{Type: "output", Data: base64.StdEncoding.EncodeToString(chunk)}); writeErr != nil {
					return
				}
			}
			_ = writeMessage(terminalSocketMessage{Type: "closed", Message: "remote terminal closed"})
		}()
		go func() {
			<-done
			_ = conn.Close()
		}()

		policy, policyErr := discoveryService.RemoteSessionEffectivePolicy(item.ID, item.Operator, item.Roles)
		if policyErr != nil {
			_ = writeMessage(terminalSocketMessage{Type: "error", Message: "远程会话策略不可用"})
			return
		}
		guard := &terminalInputGuard{}
		for {
			var message terminalSocketMessage
			if err = conn.ReadJSON(&message); err != nil {
				closeDone()
				return
			}
			switch message.Type {
			case "input":
				if access != "control" {
					_ = writeMessage(terminalSocketMessage{Type: "blocked", Message: "当前为只读协作者，不能向远程终端发送输入"})
					continue
				}
				forward, command, approvalCommand, blocked := guard.process(message.Data, policy.ApprovalMode == "all")
				_ = discoveryService.AppendTerminalEvent(item.ID, "input", message.Data)
				if forward != "" {
					if _, err = channel.Write([]byte(forward)); err != nil {
						closeDone()
						return
					}
				}
				if blocked != "" {
					discoveryService.RecordRemoteSessionEvent(item.ID, "error", "risk-blocked", approvalCommand+" | "+blocked)
					if approvalCommand == "" {
						_ = writeMessage(terminalSocketMessage{Type: "blocked", Message: blocked})
						auditService.Record(item.Operator, "remote.session.command_blocked", item.ID, approvalCommand+" | "+blocked)
					} else {
						approval, approvalErr := discoveryService.RequestTerminalApproval(item.ID, approvalCommand, item.Operator, item.Roles)
						if approvalErr != nil {
							_ = writeMessage(terminalSocketMessage{Type: "blocked", Message: blocked + " | " + approvalErr.Error()})
							auditService.Record(item.Operator, "remote.session.command_blocked", item.ID, approvalCommand+" | "+blocked)
						} else {
							_ = writeMessage(terminalSocketMessage{Type: "approval_required", Message: blocked, Approval: &approval})
							auditService.Record(item.Operator, "remote.session.command_approval_requested", item.ID, approvalCommand+" | "+blocked)
						}
					}
				}
				if command != "" {
					discoveryService.RecordRemoteSessionEvent(item.ID, "success", "interactive-command", command)
				}
			case "resize":
				_ = channel.Resize(message.Cols, message.Rows)
			case "close":
				closeDone()
				return
			}
		}
	})

	mux.HandleFunc("GET /api/v1/discovery/remote-sessions/{id}/collaborators", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		collaborators, err := discoveryService.RemoteSessionCollaborators(r.PathValue("id"), user.Username, user.Roles)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "remote session not found"})
			return
		}
		writeJSON(w, http.StatusOK, collaborators)
	})
	mux.HandleFunc("POST /api/v1/discovery/remote-sessions/{id}/collaborators", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		var input discovery.RemoteSessionCollaboratorInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid collaborator"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		collaborators, err := discoveryService.AddRemoteSessionCollaborator(r.PathValue("id"), input, user.Username, user.Roles)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		auditService.Record(user.Username, "remote.session.collaborator_add", r.PathValue("id"), input.Username+" | "+input.Access)
		writeJSON(w, http.StatusCreated, collaborators)
	})
	mux.HandleFunc("DELETE /api/v1/discovery/remote-sessions/{id}/collaborators/{username}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		collaborators, err := discoveryService.RemoveRemoteSessionCollaborator(r.PathValue("id"), r.PathValue("username"), user.Username, user.Roles)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		auditService.Record(user.Username, "remote.session.collaborator_remove", r.PathValue("id"), r.PathValue("username"))
		writeJSON(w, http.StatusOK, collaborators)
	})
	mux.HandleFunc("POST /api/v1/discovery/remote-sessions/{id}/force-disconnect", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		if err := discoveryService.ForceDisconnectRemoteSession(r.PathValue("id"), user.Username, user.Roles); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		auditService.Record(user.Username, "remote.session.force_disconnect", r.PathValue("id"), "管理员强制断开")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/discovery/remote-sessions/{id}/terminal-recording/export", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		content, err := discoveryService.ExportTerminalRecording(r.PathValue("id"), user.Username, user.Roles)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "terminal recording not found"})
			return
		}
		writeTerminalExport(w, r.PathValue("id")+"-terminal-recording.txt", content)
	})
	mux.HandleFunc("GET /api/v1/discovery/remote-sessions/{id}/command-log/export", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		content, err := discoveryService.ExportCommandLog(r.PathValue("id"), user.Username, user.Roles)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "remote command log not found"})
			return
		}
		writeTerminalExport(w, r.PathValue("id")+"-command-log.txt", content)
	})

	mux.HandleFunc("GET /api/v1/discovery/remote-terminal-approvals", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		writeJSON(w, http.StatusOK, discoveryService.TerminalApprovals(r.URL.Query().Get("sessionId"), user.Username, user.Roles))
	})
	mux.HandleFunc("POST /api/v1/discovery/remote-terminal-approvals/{id}/decision", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var input struct {
			Decision string `json:"decision"`
			Comment  string `json:"comment"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid approval decision"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.DecideTerminalApproval(r.PathValue("id"), input.Decision, input.Comment, user.Username, user.Roles)
		if err != nil {
			switch {
			case errors.Is(err, discovery.ErrRemoteForbidden):
				writeJSON(w, http.StatusForbidden, map[string]string{"message": "仅平台管理员可审批高风险命令"})
			case errors.Is(err, discovery.ErrNotFound):
				writeJSON(w, http.StatusNotFound, map[string]string{"message": "审批请求不存在"})
			default:
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			}
			return
		}
		action := "remote.session.approval_rejected"
		if item.Status == "approved" {
			action = "remote.session.approval_approved"
		}
		auditService.Record(user.Username, action, item.ID, item.AssetName+"/"+item.IP+" | "+item.Command)
		writeJSON(w, http.StatusOK, item)
	})

	mux.HandleFunc("GET /api/v1/discovery/remote-security-policies", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		writeJSON(w, http.StatusOK, discoveryService.RemoteSecurityPolicies())
	})
	mux.HandleFunc("GET /api/v1/discovery/remote-security-policies/effective", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		writeJSON(w, http.StatusOK, discoveryService.EffectiveRemoteSecurityPolicy(user.Username, user.Roles))
	})
	mux.HandleFunc("POST /api/v1/discovery/remote-security-policies", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var input discovery.RemoteSecurityPolicyInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid remote security policy"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.SaveRemoteSecurityPolicy(input, user.Username)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		auditService.Record(user.Username, "remote.policy.save", item.ID, item.SubjectType+"/"+item.Subject)
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("GET /api/v1/discovery/remote-security-policies/audit", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 200 {
			limit = 100
		}
		entries, err := auditService.List(500)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "AUDIT_ERROR", "message": err.Error()})
			return
		}
		filtered := make([]audit.Entry, 0, limit)
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Action, "remote.policy.") && !strings.HasPrefix(entry.Action, "remote.policy_template.") {
				continue
			}
			filtered = append(filtered, entry)
			if len(filtered) >= limit {
				break
			}
		}
		writeJSON(w, http.StatusOK, filtered)
	})
	mux.HandleFunc("POST /api/v1/discovery/remote-security-policies/batch", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var input discovery.RemoteSecurityPolicyBatchInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid remote security policy batch"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		result, err := discoveryService.SaveRemoteSecurityPolicyBatch(input, user.Username)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		for _, item := range result.Applied {
			auditService.Record(user.Username, "remote.policy.batch_apply", item.ID, item.SubjectType+"/"+item.Subject)
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /api/v1/discovery/remote-security-policy-templates", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		writeJSON(w, http.StatusOK, discoveryService.RemoteSecurityPolicyTemplates())
	})
	mux.HandleFunc("POST /api/v1/discovery/remote-security-policy-templates", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var input discovery.RemoteSecurityPolicyTemplateInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid security policy template"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.SaveRemoteSecurityPolicyTemplate(input, user.Username)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		action := "remote.policy_template.create"
		if input.ID != "" {
			action = "remote.policy_template.update"
		}
		auditService.Record(user.Username, action, item.ID, item.Name+" | v"+strconv.Itoa(item.CurrentVersion))
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("GET /api/v1/discovery/remote-security-policy-templates/{id}/versions", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		versions, err := discoveryService.RemoteSecurityPolicyTemplateVersions(r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "security policy template not found"})
			return
		}
		writeJSON(w, http.StatusOK, versions)
	})
	mux.HandleFunc("POST /api/v1/discovery/remote-security-policy-templates/{id}/toggle", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.ToggleRemoteSecurityPolicyTemplate(r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "security policy template not found"})
			return
		}
		auditService.Record(user.Username, "remote.policy_template.toggle", item.ID, item.Name+" | enabled="+strconv.FormatBool(item.Enabled))
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("POST /api/v1/discovery/remote-security-policy-templates/{id}/rollback", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var input struct {
			Version    int    `json:"version"`
			ChangeNote string `json:"changeNote"`
		}
		if decodeJSON(r, &input) != nil || input.Version < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid rollback request"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.RollbackRemoteSecurityPolicyTemplate(r.PathValue("id"), input.Version, input.ChangeNote, user.Username)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		auditService.Record(user.Username, "remote.policy_template.rollback", item.ID, "v"+strconv.Itoa(input.Version)+" -> v"+strconv.Itoa(item.CurrentVersion))
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("DELETE /api/v1/discovery/remote-security-policy-templates/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		if err := discoveryService.DeleteRemoteSecurityPolicyTemplate(r.PathValue("id")); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		auditService.Record(user.Username, "remote.policy_template.delete", r.PathValue("id"), "")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /api/v1/discovery/remote-security-policies/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		if err := discoveryService.DeleteRemoteSecurityPolicy(r.PathValue("id")); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		auditService.Record(user.Username, "remote.policy.delete", r.PathValue("id"), "")
		w.WriteHeader(http.StatusNoContent)
	})
}

func remoteGrantAuditDetail(item discovery.AccessGrant) string {
	scope := []string{}
	if len(item.ProjectGroups) > 0 {
		scope = append(scope, "groups="+strings.Join(item.ProjectGroups, ","))
	} else if item.ProjectGroup != "" {
		scope = append(scope, "group="+item.ProjectGroup)
	}
	if len(item.Tags) > 0 {
		scope = append(scope, "tags="+strings.Join(item.Tags, ","))
	}
	if len(item.AssetIDs) > 0 {
		scope = append(scope, "assets="+strings.Join(item.AssetIDs, ","))
	} else if item.AssetID != "" {
		scope = append(scope, "asset="+item.AssetID)
	}
	if len(scope) == 0 {
		scope = append(scope, "all")
	}
	return item.Subject + " | " + strings.Join(scope, " | ")
}

func writeTerminalExport(w http.ResponseWriter, filename string, content []byte) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}
