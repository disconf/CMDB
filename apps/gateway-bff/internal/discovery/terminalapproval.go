package discovery

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const terminalApprovalTTL = 10 * time.Minute

var ErrRemoteForbidden = errors.New("remote operation forbidden")

// TerminalApproval is a one-time authorization request for a command that the
// terminal risk guard would otherwise reject.
type TerminalApproval struct {
	ID              string `json:"id"`
	SessionID       string `json:"sessionId"`
	AssetID         string `json:"assetId"`
	AssetName       string `json:"assetName"`
	IP              string `json:"ip"`
	Operator        string `json:"operator"`
	Command         string `json:"command"`
	Reason          string `json:"reason"`
	Status          string `json:"status"`
	RequestedAt     string `json:"requestedAt"`
	ExpiresAt       string `json:"expiresAt"`
	DecidedAt       string `json:"decidedAt,omitempty"`
	Approver        string `json:"approver,omitempty"`
	DecisionComment string `json:"decisionComment,omitempty"`
}

func terminalApprovalID() string {
	data := make([]byte, 10)
	_, _ = rand.Read(data)
	return "rapr-" + hex.EncodeToString(data)
}

func (s *Service) RequestTerminalApproval(sessionID, command, operator string, roles []string) (TerminalApproval, error) {
	command = strings.TrimSpace(command)
	if command == "" || len(command) > 4000 {
		return TerminalApproval{}, ErrRemoteValidation
	}
	reason := BlockedTerminalCommand(command)
	if reason == "" {
		return TerminalApproval{}, ErrRemoteValidation
	}
	item, _, err := s.sessionForOperator(sessionID, operator, roles)
	if err != nil {
		return TerminalApproval{}, err
	}

	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireTerminalApprovalsLocked(now)
	for _, existing := range s.terminalApprovals {
		if existing.SessionID == sessionID && existing.Operator == operator && existing.Status == "pending" {
			if existing.Command == command {
				return existing, nil
			}
			return TerminalApproval{}, ErrRemoteValidation
		}
	}
	approval := TerminalApproval{
		ID:          terminalApprovalID(),
		SessionID:   item.ID,
		AssetID:     item.AssetID,
		AssetName:   item.AssetName,
		IP:          item.IP,
		Operator:    operator,
		Command:     command,
		Reason:      reason,
		Status:      "pending",
		RequestedAt: now.Format("2006-01-02 15:04:05"),
		ExpiresAt:   now.Add(terminalApprovalTTL).Format("2006-01-02 15:04:05"),
	}
	s.terminalApprovals = append([]TerminalApproval{approval}, s.terminalApprovals...)
	if s.db != nil {
		_ = s.persistTerminalApproval(approval)
	}
	return approval, nil
}

func (s *Service) TerminalApprovals(sessionID, username string, roles []string) []TerminalApproval {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireTerminalApprovalsLocked(time.Now())
	admin := remoteContainsString(roles, "admin") || remoteContainsString(roles, "platform-admin")
	out := make([]TerminalApproval, 0, len(s.terminalApprovals))
	for _, item := range s.terminalApprovals {
		if sessionID != "" && item.SessionID != sessionID {
			continue
		}
		if !admin && item.Operator != username {
			continue
		}
		out = append(out, item)
		if len(out) >= 200 {
			break
		}
	}
	return out
}

func (s *Service) DecideTerminalApproval(id, decision, comment, approver string, roles []string) (TerminalApproval, error) {
	decision = strings.ToLower(strings.TrimSpace(decision))
	if decision != "approve" && decision != "reject" {
		return TerminalApproval{}, ErrRemoteValidation
	}
	if !remoteContainsString(roles, "admin") && !remoteContainsString(roles, "platform-admin") {
		return TerminalApproval{}, ErrRemoteForbidden
	}
	comment = strings.TrimSpace(comment)
	if len(comment) > 500 {
		comment = comment[:500]
	}

	now := time.Now()
	s.mu.Lock()
	s.expireTerminalApprovalsLocked(now)
	index := -1
	for i := range s.terminalApprovals {
		if s.terminalApprovals[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		s.mu.Unlock()
		return TerminalApproval{}, ErrNotFound
	}
	item := s.terminalApprovals[index]
	if item.Status != "pending" {
		s.mu.Unlock()
		return item, ErrRemoteValidation
	}
	if decision == "reject" {
		item.Status = "rejected"
		item.DecidedAt = now.Format("2006-01-02 15:04:05")
		item.Approver = approver
		item.DecisionComment = comment
		s.terminalApprovals[index] = item
		if s.db != nil {
			_ = s.persistTerminalApproval(item)
		}
		s.mu.Unlock()
		s.RecordRemoteSessionEvent(item.SessionID, "error", "command-rejected", "审批拒绝: "+item.Command)
		return item, nil
	}

	channel := s.terminals[item.SessionID]
	if channel == nil {
		item.Status = "expired"
		item.DecidedAt = now.Format("2006-01-02 15:04:05")
		item.Approver = approver
		item.DecisionComment = "终端连接已断开，审批请求自动失效"
		s.terminalApprovals[index] = item
		if s.db != nil {
			_ = s.persistTerminalApproval(item)
		}
		s.mu.Unlock()
		return item, ErrRemoteValidation
	}

	// Mark approved before writing so two approvers cannot release the same
	// command twice.
	item.Status = "approved"
	item.DecidedAt = now.Format("2006-01-02 15:04:05")
	item.Approver = approver
	item.DecisionComment = comment
	s.terminalApprovals[index] = item
	if s.db != nil {
		_ = s.persistTerminalApproval(item)
	}
	s.mu.Unlock()

	if _, err := channel.Write([]byte(item.Command + "\r")); err != nil {
		s.mu.Lock()
		for i := range s.terminalApprovals {
			if s.terminalApprovals[i].ID == id {
				s.terminalApprovals[i].Status = "failed"
				s.terminalApprovals[i].DecisionComment = "放行写入失败: " + err.Error()
				if s.db != nil {
					_ = s.persistTerminalApproval(s.terminalApprovals[i])
				}
				break
			}
		}
		s.mu.Unlock()
		return item, err
	}
	s.RecordRemoteSessionEvent(item.SessionID, "success", "command-approved", "审批通过并执行: "+item.Command)
	return item, nil
}

func (s *Service) expireTerminalApprovalsLocked(now time.Time) {
	for i := range s.terminalApprovals {
		item := &s.terminalApprovals[i]
		if item.Status != "pending" || parseRemoteTime(item.ExpiresAt).After(now) {
			continue
		}
		item.Status = "expired"
		item.DecidedAt = now.Format("2006-01-02 15:04:05")
		item.DecisionComment = "审批请求已超时"
		if s.db != nil {
			_ = s.persistTerminalApproval(*item)
		}
	}
}
func (s *Service) loadTerminalApprovals() error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.Query(`SELECT id,session_id,asset_id,asset_name,host,operator_name,command,reason,status,requested_at,expires_at,decided_at,approver,decision_comment FROM remote_terminal_approvals ORDER BY updated_at DESC LIMIT 500`)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []TerminalApproval{}
	for rows.Next() {
		var item TerminalApproval
		if err = rows.Scan(&item.ID, &item.SessionID, &item.AssetID, &item.AssetName, &item.IP, &item.Operator, &item.Command, &item.Reason, &item.Status, &item.RequestedAt, &item.ExpiresAt, &item.DecidedAt, &item.Approver, &item.DecisionComment); err != nil {
			return err
		}
		out = append(out, item)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	s.terminalApprovals = out
	s.expireTerminalApprovalsLocked(time.Now())
	return nil
}

func (s *Service) persistTerminalApproval(item TerminalApproval) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO remote_terminal_approvals(id,session_id,asset_id,asset_name,host,operator_name,command,reason,status,requested_at,expires_at,decided_at,approver,decision_comment,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,now()) ON CONFLICT(id) DO UPDATE SET session_id=excluded.session_id,asset_id=excluded.asset_id,asset_name=excluded.asset_name,host=excluded.host,operator_name=excluded.operator_name,command=excluded.command,reason=excluded.reason,status=excluded.status,requested_at=excluded.requested_at,expires_at=excluded.expires_at,decided_at=excluded.decided_at,approver=excluded.approver,decision_comment=excluded.decision_comment,updated_at=now()`, item.ID, item.SessionID, item.AssetID, item.AssetName, item.IP, item.Operator, item.Command, item.Reason, item.Status, item.RequestedAt, item.ExpiresAt, item.DecidedAt, item.Approver, item.DecisionComment)
	return err
}
