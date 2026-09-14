package discovery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"path"
	"strconv"
	"strings"
	"time"
)

type RemoteSecurityPolicy struct {
	ID                        string   `json:"id"`
	Name                      string   `json:"name"`
	SubjectType               string   `json:"subjectType"`
	Subject                   string   `json:"subject"`
	Enabled                   bool     `json:"enabled"`
	Priority                  int      `json:"priority"`
	MaxSessionMinutes         int      `json:"maxSessionMinutes"`
	MaxConcurrentSessions     int      `json:"maxConcurrentSessions"`
	RecordingRetentionDays    int      `json:"recordingRetentionDays"`
	FileTransferEnabled       bool     `json:"fileTransferEnabled"`
	UploadMaxMB               int      `json:"uploadMaxMB"`
	DownloadMaxMB             int      `json:"downloadMaxMB"`
	AllowedUploadPaths        []string `json:"allowedUploadPaths"`
	AllowedDownloadPaths      []string `json:"allowedDownloadPaths"`
	AllowControlCollaborators bool     `json:"allowControlCollaborators"`
	ApprovalMode              string   `json:"approvalMode"`
	ApprovalTTLMinutes        int      `json:"approvalTtlMinutes"`
	CreatedBy                 string   `json:"createdBy"`
	UpdatedBy                 string   `json:"updatedBy"`
	UpdatedAt                 string   `json:"updatedAt"`
}

type RemoteSecurityPolicyInput struct {
	ID                        string   `json:"id"`
	Name                      string   `json:"name"`
	SubjectType               string   `json:"subjectType"`
	Subject                   string   `json:"subject"`
	Enabled                   *bool    `json:"enabled"`
	MaxSessionMinutes         int      `json:"maxSessionMinutes"`
	MaxConcurrentSessions     int      `json:"maxConcurrentSessions"`
	RecordingRetentionDays    int      `json:"recordingRetentionDays"`
	FileTransferEnabled       bool     `json:"fileTransferEnabled"`
	UploadMaxMB               int      `json:"uploadMaxMb"`
	DownloadMaxMB             int      `json:"downloadMaxMb"`
	AllowedUploadPaths        []string `json:"allowedUploadPaths"`
	AllowedDownloadPaths      []string `json:"allowedDownloadPaths"`
	AllowControlCollaborators bool     `json:"allowControlCollaborators"`
	ApprovalMode              string   `json:"approvalMode"`
	ApprovalTTLMinutes        int      `json:"approvalTtlMinutes"`
}

type RemoteSecurityPolicySubjectInput struct {
	SubjectType string `json:"subjectType"`
	Subject     string `json:"subject"`
}

type RemoteSecurityPolicyBatchInput struct {
	Policy   RemoteSecurityPolicyInput          `json:"policy"`
	Subjects []RemoteSecurityPolicySubjectInput `json:"subjects"`
}

type RemoteSecurityPolicyBatchError struct {
	SubjectType string `json:"subjectType"`
	Subject     string `json:"subject"`
	Message     string `json:"message"`
}

type RemoteSecurityPolicyBatchResult struct {
	Applied []RemoteSecurityPolicy           `json:"applied"`
	Errors  []RemoteSecurityPolicyBatchError `json:"errors"`
}

func remoteSecurityPolicyID() string {
	data := make([]byte, 8)
	_, _ = rand.Read(data)
	return "rpolicy-" + hex.EncodeToString(data)
}

func defaultRemoteSecurityPolicy() RemoteSecurityPolicy {
	return RemoteSecurityPolicy{
		ID:                        "rpolicy-global-default",
		Name:                      "默认远程运维策略",
		SubjectType:               "global",
		Subject:                   "global",
		Enabled:                   true,
		Priority:                  100,
		MaxSessionMinutes:         30,
		MaxConcurrentSessions:     20,
		RecordingRetentionDays:    180,
		FileTransferEnabled:       true,
		UploadMaxMB:               50,
		DownloadMaxMB:             25,
		AllowedUploadPaths:        []string{"/**"},
		AllowedDownloadPaths:      []string{"/**"},
		AllowControlCollaborators: true,
		ApprovalMode:              "risk",
		ApprovalTTLMinutes:        10,
		UpdatedAt:                 time.Now().Format("2006-01-02 15:04:05"),
	}
}

func (s *Service) RemoteSecurityPolicies() []RemoteSecurityPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneRemoteSecurityPolicies(s.remotePolicies)
}

func (s *Service) EffectiveRemoteSecurityPolicy(username string, roles []string) RemoteSecurityPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return effectiveRemoteSecurityPolicy(s.remotePolicies, username, roles)
}

func (s *Service) RemoteSessionEffectivePolicy(sessionID, username string, roles []string) (RemoteSecurityPolicy, error) {
	_, _, err := s.sessionForOperator(sessionID, username, roles)
	if err != nil {
		return RemoteSecurityPolicy{}, err
	}
	return s.EffectiveRemoteSecurityPolicy(username, roles), nil
}

func (s *Service) SaveRemoteSecurityPolicy(input RemoteSecurityPolicyInput, actor string) (RemoteSecurityPolicy, error) {
	item, err := normalizeRemoteSecurityPolicy(input, actor)
	if err != nil {
		return RemoteSecurityPolicy{}, err
	}
	s.mu.Lock()
	if item.ID != "" {
		for i := range s.remotePolicies {
			if s.remotePolicies[i].ID != item.ID {
				continue
			}
			item.CreatedBy = s.remotePolicies[i].CreatedBy
			s.remotePolicies[i] = item
			s.mu.Unlock()
			if s.db != nil {
				err = s.persistRemoteSecurityPolicy(item)
			}
			return cloneRemoteSecurityPolicy(item), err
		}
	}
	if item.ID == "" {
		item.ID = remoteSecurityPolicyID()
	}
	for i := range s.remotePolicies {
		if s.remotePolicies[i].SubjectType == item.SubjectType && s.remotePolicies[i].Subject == item.Subject {
			item.ID = s.remotePolicies[i].ID
			item.CreatedBy = s.remotePolicies[i].CreatedBy
			s.remotePolicies[i] = item
			s.mu.Unlock()
			if s.db != nil {
				err = s.persistRemoteSecurityPolicy(item)
			}
			return cloneRemoteSecurityPolicy(item), err
		}
	}
	s.remotePolicies = append(s.remotePolicies, item)
	s.mu.Unlock()
	if s.db != nil {
		err = s.persistRemoteSecurityPolicy(item)
	}
	return cloneRemoteSecurityPolicy(item), err
}

func (s *Service) SaveRemoteSecurityPolicyBatch(input RemoteSecurityPolicyBatchInput, actor string) (RemoteSecurityPolicyBatchResult, error) {
	result := RemoteSecurityPolicyBatchResult{Applied: []RemoteSecurityPolicy{}, Errors: []RemoteSecurityPolicyBatchError{}}
	if len(input.Subjects) == 0 || len(input.Subjects) > 100 {
		return result, fmt.Errorf("%w: batch subjects must contain 1 to 100 items", ErrRemoteValidation)
	}
	seen := map[string]struct{}{}
	for _, subject := range input.Subjects {
		subjectType := strings.ToLower(strings.TrimSpace(subject.SubjectType))
		subjectName := strings.ToLower(strings.TrimSpace(subject.Subject))
		if subjectType != "user" && subjectType != "role" {
			result.Errors = append(result.Errors, RemoteSecurityPolicyBatchError{SubjectType: subject.SubjectType, Subject: subject.Subject, Message: "只支持用户或角色策略"})
			continue
		}
		if subjectName == "" {
			result.Errors = append(result.Errors, RemoteSecurityPolicyBatchError{SubjectType: subjectType, Subject: subject.Subject, Message: "策略对象不能为空"})
			continue
		}
		key := subjectType + ":" + subjectName
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		itemInput := input.Policy
		itemInput.ID = ""
		itemInput.SubjectType = subjectType
		itemInput.Subject = subjectName
		item, err := s.SaveRemoteSecurityPolicy(itemInput, actor)
		if err != nil {
			result.Errors = append(result.Errors, RemoteSecurityPolicyBatchError{SubjectType: subjectType, Subject: subjectName, Message: err.Error()})
			continue
		}
		result.Applied = append(result.Applied, item)
	}
	return result, nil
}

func (s *Service) DeleteRemoteSecurityPolicy(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrRemoteValidation
	}
	s.mu.Lock()
	found := false
	for i := range s.remotePolicies {
		if s.remotePolicies[i].ID != id {
			continue
		}
		if s.remotePolicies[i].SubjectType == "global" {
			s.mu.Unlock()
			return fmt.Errorf("%w: global remote security policy cannot be deleted", ErrRemoteValidation)
		}
		s.remotePolicies = append(s.remotePolicies[:i], s.remotePolicies[i+1:]...)
		found = true
		break
	}
	s.mu.Unlock()
	if !found {
		return ErrNotFound
	}
	if s.db != nil {
		_, err := s.db.Exec(`DELETE FROM remote_security_policies WHERE id=$1`, id)
		return err
	}
	return nil
}

func normalizeRemoteSecurityPolicy(input RemoteSecurityPolicyInput, actor string) (RemoteSecurityPolicy, error) {
	item := defaultRemoteSecurityPolicy()
	item.ID = strings.TrimSpace(input.ID)
	item.Name = strings.TrimSpace(input.Name)
	item.SubjectType = strings.ToLower(strings.TrimSpace(input.SubjectType))
	item.Subject = strings.ToLower(strings.TrimSpace(input.Subject))
	if item.SubjectType == "global" {
		item.Subject = "global"
	}
	if item.Name == "" || (item.SubjectType != "global" && item.SubjectType != "user" && item.SubjectType != "role") || item.Subject == "" {
		return RemoteSecurityPolicy{}, ErrRemoteValidation
	}
	switch item.SubjectType {
	case "user":
		item.Priority = 300
	case "role":
		item.Priority = 200
	default:
		item.Priority = 100
	}
	item.Enabled = true
	if input.Enabled != nil {
		item.Enabled = *input.Enabled
	}
	if input.MaxSessionMinutes >= 5 && input.MaxSessionMinutes <= 1440 {
		item.MaxSessionMinutes = input.MaxSessionMinutes
	} else {
		return RemoteSecurityPolicy{}, ErrRemoteValidation
	}
	if input.MaxConcurrentSessions >= 1 && input.MaxConcurrentSessions <= 100 {
		item.MaxConcurrentSessions = input.MaxConcurrentSessions
	} else {
		return RemoteSecurityPolicy{}, ErrRemoteValidation
	}
	if input.RecordingRetentionDays >= 1 && input.RecordingRetentionDays <= 3650 {
		item.RecordingRetentionDays = input.RecordingRetentionDays
	} else {
		return RemoteSecurityPolicy{}, ErrRemoteValidation
	}
	item.FileTransferEnabled = input.FileTransferEnabled
	if input.UploadMaxMB >= 1 && input.UploadMaxMB <= 50 && input.DownloadMaxMB >= 1 && input.DownloadMaxMB <= 25 {
		item.UploadMaxMB = input.UploadMaxMB
		item.DownloadMaxMB = input.DownloadMaxMB
	} else {
		return RemoteSecurityPolicy{}, ErrRemoteValidation
	}
	item.AllowedUploadPaths = normalizePolicyPaths(input.AllowedUploadPaths)
	item.AllowedDownloadPaths = normalizePolicyPaths(input.AllowedDownloadPaths)
	item.AllowControlCollaborators = input.AllowControlCollaborators
	item.ApprovalMode = strings.ToLower(strings.TrimSpace(input.ApprovalMode))
	if item.ApprovalMode != "risk" && item.ApprovalMode != "all" {
		return RemoteSecurityPolicy{}, ErrRemoteValidation
	}
	if input.ApprovalTTLMinutes >= 1 && input.ApprovalTTLMinutes <= 60 {
		item.ApprovalTTLMinutes = input.ApprovalTTLMinutes
	} else {
		return RemoteSecurityPolicy{}, ErrRemoteValidation
	}
	item.CreatedBy = actor
	item.UpdatedBy = actor
	item.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	return item, nil
}

func normalizePolicyPaths(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 512 {
			continue
		}
		if value != "*" && !strings.HasPrefix(value, "/") {
			continue
		}
		if !remoteContainsString(out, value) {
			out = append(out, value)
		}
		if len(out) >= 20 {
			break
		}
	}
	if len(out) == 0 {
		return []string{"/**"}
	}
	return out
}

func effectiveRemoteSecurityPolicy(policies []RemoteSecurityPolicy, username string, roles []string) RemoteSecurityPolicy {
	effective := defaultRemoteSecurityPolicy()
	bestPriority := -1
	var bestUpdated time.Time
	for _, item := range policies {
		if !item.Enabled {
			continue
		}
		match := item.SubjectType == "global" || (item.SubjectType == "user" && item.Subject == strings.ToLower(username)) || (item.SubjectType == "role" && remoteContainsString(roles, item.Subject))
		if !match || item.Priority < bestPriority {
			continue
		}
		updated, _ := time.ParseInLocation("2006-01-02 15:04:05", item.UpdatedAt, time.Local)
		if item.Priority == bestPriority && updated.Before(bestUpdated) {
			continue
		}
		effective = cloneRemoteSecurityPolicy(item)
		bestPriority = item.Priority
		bestUpdated = updated
	}
	return effective
}

func cloneRemoteSecurityPolicies(items []RemoteSecurityPolicy) []RemoteSecurityPolicy {
	out := make([]RemoteSecurityPolicy, 0, len(items))
	for _, item := range items {
		out = append(out, cloneRemoteSecurityPolicy(item))
	}
	return out
}

func cloneRemoteSecurityPolicy(item RemoteSecurityPolicy) RemoteSecurityPolicy {
	item.AllowedUploadPaths = append([]string(nil), item.AllowedUploadPaths...)
	item.AllowedDownloadPaths = append([]string(nil), item.AllowedDownloadPaths...)
	if item.AllowedUploadPaths == nil {
		item.AllowedUploadPaths = []string{"/**"}
	}
	if item.AllowedDownloadPaths == nil {
		item.AllowedDownloadPaths = []string{"/**"}
	}
	return item
}

func remotePathAllowed(remotePath string, patterns []string) bool {
	remotePath = path.Clean(strings.TrimSpace(remotePath))
	if remotePath == "." || !strings.HasPrefix(remotePath, "/") {
		return false
	}
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "*" || pattern == "/**" {
			return true
		}
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "/**")
			if prefix == "" || remotePath == prefix || strings.HasPrefix(remotePath, prefix+"/") {
				return true
			}
			continue
		}
		if strings.HasSuffix(pattern, "/*") {
			prefix := strings.TrimSuffix(pattern, "/*")
			if strings.HasPrefix(remotePath, prefix+"/") && !strings.Contains(strings.TrimPrefix(remotePath, prefix+"/"), "/") {
				return true
			}
			continue
		}
		if remotePath == path.Clean(pattern) {
			return true
		}
	}
	return false
}

func (s *Service) checkRemoteFilePolicy(item RemoteSession, operator string, roles []string, remotePath string, upload bool, size int64) error {
	policy := s.EffectiveRemoteSecurityPolicy(operator, roles)
	if !policy.FileTransferEnabled {
		return fmt.Errorf("%w: file transfer is disabled by policy", ErrRemoteValidation)
	}
	maxMB := policy.DownloadMaxMB
	paths := policy.AllowedDownloadPaths
	action := "download"
	if upload {
		maxMB = policy.UploadMaxMB
		paths = policy.AllowedUploadPaths
		action = "upload"
	}
	if size > int64(maxMB)<<20 {
		return fmt.Errorf("%w: %s exceeds policy limit %dMB", ErrRemoteValidation, action, maxMB)
	}
	if !remotePathAllowed(remotePath, paths) {
		return fmt.Errorf("%w: %s path is not allowed by policy", ErrRemoteValidation, action)
	}
	_ = item
	return nil
}

func (s *Service) PurgeExpiredTerminalEvents() (int64, error) {
	if s.db == nil {
		return 0, nil
	}
	result, err := s.db.Exec(`DELETE FROM remote_terminal_events e USING remote_access_sessions a WHERE e.session_id=a.id AND a.status<>'active' AND a.archive_status='expired'`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Service) RunTerminalRetention(ctx context.Context) {
	maintain := func() {
		archived, err := s.ArchivePendingRemoteSessions(ctx)
		if err != nil {
			slog.Error("archive terminal recordings", "error", err)
		} else if archived > 0 {
			slog.Info("archived terminal recordings", "count", archived)
		}
		expired, err := s.ExpireRemoteSessionArchives(ctx)
		if err != nil {
			slog.Error("expire terminal recording archives", "error", err)
		}
		if expired > 0 {
			slog.Info("expired terminal recording archives", "count", expired)
		}
		removed, err := s.PurgeExpiredTerminalEvents()
		if err != nil {
			slog.Error("purge expired terminal events", "error", err)
			return
		}
		if removed > 0 {
			slog.Info("purged expired terminal events", "count", removed)
		}
	}
	maintain()
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			maintain()
		}
	}
}

func (s *Service) loadRemoteSecurityPolicies() error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.Query(`SELECT id,name,subject_type,subject,enabled,priority,max_session_minutes,max_concurrent_sessions,recording_retention_days,file_transfer_enabled,upload_max_mb,download_max_mb,allowed_upload_paths,allowed_download_paths,allow_control_collaborators,approval_mode,approval_ttl_minutes,created_by,updated_by,to_char(updated_at,'YYYY-MM-DD HH24:MI:SS') FROM remote_security_policies ORDER BY priority DESC,updated_at DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []RemoteSecurityPolicy{}
	for rows.Next() {
		var item RemoteSecurityPolicy
		var uploadPaths, downloadPaths []byte
		if err = rows.Scan(&item.ID, &item.Name, &item.SubjectType, &item.Subject, &item.Enabled, &item.Priority, &item.MaxSessionMinutes, &item.MaxConcurrentSessions, &item.RecordingRetentionDays, &item.FileTransferEnabled, &item.UploadMaxMB, &item.DownloadMaxMB, &uploadPaths, &downloadPaths, &item.AllowControlCollaborators, &item.ApprovalMode, &item.ApprovalTTLMinutes, &item.CreatedBy, &item.UpdatedBy, &item.UpdatedAt); err != nil {
			return err
		}
		_ = json.Unmarshal(uploadPaths, &item.AllowedUploadPaths)
		_ = json.Unmarshal(downloadPaths, &item.AllowedDownloadPaths)
		items = append(items, cloneRemoteSecurityPolicy(item))
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(items) == 0 {
		item := defaultRemoteSecurityPolicy()
		if err = s.persistRemoteSecurityPolicy(item); err != nil {
			return err
		}
		items = append(items, item)
	}
	s.remotePolicies = items
	return nil
}

func (s *Service) persistRemoteSecurityPolicy(item RemoteSecurityPolicy) error {
	if s.db == nil {
		return nil
	}
	uploadPaths, _ := json.Marshal(normalizePolicyPaths(item.AllowedUploadPaths))
	downloadPaths, _ := json.Marshal(normalizePolicyPaths(item.AllowedDownloadPaths))
	_, err := s.db.Exec(`INSERT INTO remote_security_policies(id,name,subject_type,subject,enabled,priority,max_session_minutes,max_concurrent_sessions,recording_retention_days,file_transfer_enabled,upload_max_mb,download_max_mb,allowed_upload_paths,allowed_download_paths,allow_control_collaborators,approval_mode,approval_ttl_minutes,created_by,updated_by,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,now()) ON CONFLICT(id) DO UPDATE SET name=excluded.name,subject_type=excluded.subject_type,subject=excluded.subject,enabled=excluded.enabled,priority=excluded.priority,max_session_minutes=excluded.max_session_minutes,max_concurrent_sessions=excluded.max_concurrent_sessions,recording_retention_days=excluded.recording_retention_days,file_transfer_enabled=excluded.file_transfer_enabled,upload_max_mb=excluded.upload_max_mb,download_max_mb=excluded.download_max_mb,allowed_upload_paths=excluded.allowed_upload_paths,allowed_download_paths=excluded.allowed_download_paths,allow_control_collaborators=excluded.allow_control_collaborators,approval_mode=excluded.approval_mode,approval_ttl_minutes=excluded.approval_ttl_minutes,updated_by=excluded.updated_by,updated_at=now()`, item.ID, item.Name, item.SubjectType, item.Subject, item.Enabled, item.Priority, item.MaxSessionMinutes, item.MaxConcurrentSessions, item.RecordingRetentionDays, item.FileTransferEnabled, item.UploadMaxMB, item.DownloadMaxMB, uploadPaths, downloadPaths, item.AllowControlCollaborators, item.ApprovalMode, item.ApprovalTTLMinutes, item.CreatedBy, item.UpdatedBy)
	return err
}

func policyPathSummary(values []string) string {
	if len(values) == 0 {
		return "/**"
	}
	return strings.Join(values, ", ")
}

func policyLimitSummary(upload, download int) string {
	return strconv.Itoa(upload) + "MB / " + strconv.Itoa(download) + "MB"
}
