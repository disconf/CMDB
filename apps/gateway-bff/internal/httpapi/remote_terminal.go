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
	Cols     int                         `json:"cols,omitempty"`
	Rows     int                         `json:"rows,omitempty"`
	Approval *discovery.TerminalApproval `json:"approval,omitempty"`
}

type terminalInputGuard struct {
	buffer     []rune
	skipEscape bool
}

func (g *terminalInputGuard) process(data string) (forward string, command string, approvalCommand string, blocked string) {
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
			if reason := discovery.BlockedTerminalCommand(line); reason != "" {
				out.WriteByte(3)
				approvalCommand = line
				blocked = reason
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
		if !authorize(w, r, authService, "discovery:manage") {
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
		channel, err := discoveryService.OpenRemoteTerminal(item.ID, item.Operator, item.Roles, cols, rows)
		if err != nil {
			_ = conn.WriteJSON(terminalSocketMessage{Type: "error", Message: err.Error()})
			return
		}
		defer channel.Close()
		auditService.Record(item.Operator, "remote.session.terminal_open", item.ID, item.AssetName+"/"+item.IP)
		discoveryService.RecordRemoteSessionEvent(item.ID, "success", "terminal-open", "交互式终端已连接")
		defer discoveryService.RecordRemoteSessionEvent(item.ID, "success", "terminal-close", "交互式终端已断开")

		var writeMu sync.Mutex
		writeMessage := func(message terminalSocketMessage) error {
			writeMu.Lock()
			defer writeMu.Unlock()
			return conn.WriteJSON(message)
		}
		done := make(chan struct{})
		var doneOnce sync.Once
		closeDone := func() { doneOnce.Do(func() { close(done) }) }
		go func() {
			defer closeDone()
			buffer := make([]byte, 8192)
			for {
				count, readErr := channel.Read(buffer)
				if count > 0 {
					chunk := append([]byte(nil), buffer[:count]...)
					_ = discoveryService.AppendTerminalEvent(item.ID, "output", string(chunk))
					_ = writeMessage(terminalSocketMessage{Type: "output", Data: base64.StdEncoding.EncodeToString(chunk)})
				}
				if readErr != nil {
					_ = writeMessage(terminalSocketMessage{Type: "closed", Message: readErr.Error()})
					return
				}
			}
		}()
		go func() {
			<-done
			_ = conn.Close()
		}()

		guard := &terminalInputGuard{}
		for {
			var message terminalSocketMessage
			if err = conn.ReadJSON(&message); err != nil {
				closeDone()
				return
			}
			switch message.Type {
			case "input":
				forward, command, approvalCommand, blocked := guard.process(message.Data)
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
}
