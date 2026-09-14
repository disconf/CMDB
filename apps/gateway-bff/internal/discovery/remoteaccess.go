package discovery

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"path"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type AccessGrant struct {
	ID            string   `json:"id"`
	SubjectType   string   `json:"subjectType"`
	Subject       string   `json:"subject"`
	AssetID       string   `json:"assetId"`
	ProjectGroup  string   `json:"projectGroup"`
	AssetIDs      []string `json:"assetIds,omitempty"`
	ProjectGroups []string `json:"projectGroups,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Permissions   []string `json:"permissions"`
	Enabled       bool     `json:"enabled"`
	CreatedBy     string   `json:"createdBy"`
}

type AccessGrantInput struct {
	SubjectType   string   `json:"subjectType"`
	Subject       string   `json:"subject"`
	AssetID       string   `json:"assetId"`
	ProjectGroup  string   `json:"projectGroup"`
	AssetIDs      []string `json:"assetIds,omitempty"`
	ProjectGroups []string `json:"projectGroups,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Permissions   []string `json:"permissions"`
	Enabled       *bool    `json:"enabled"`
}

type RemoteHostKey struct {
	ID          string `json:"id"`
	AssetID     string `json:"assetId"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	KeyType     string `json:"keyType"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"publicKey"`
	AddedBy     string `json:"addedBy"`
	CreatedAt   string `json:"createdAt"`
}
type RemoteHostKeyInput struct {
	AssetID     string `json:"assetId"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	KeyType     string `json:"keyType"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"publicKey"`
}
type RemoteHostKeyProbe struct {
	AssetID            string `json:"assetId"`
	Host               string `json:"host"`
	Port               int    `json:"port"`
	KeyType            string `json:"keyType"`
	Fingerprint        string `json:"fingerprint"`
	PublicKey          string `json:"publicKey"`
	Trusted            bool   `json:"trusted"`
	Changed            bool   `json:"changed"`
	TrustedFingerprint string `json:"trustedFingerprint"`
}
type RemoteSessionLog struct {
	Time       string `json:"time"`
	Level      string `json:"level"`
	Kind       string `json:"kind,omitempty"`
	Message    string `json:"message"`
	DurationMS int64  `json:"durationMs,omitempty"`
}

type RemoteSessionCollaborator struct {
	Username string `json:"username"`
	Access   string `json:"access"`
	AddedBy  string `json:"addedBy"`
	AddedAt  string `json:"addedAt"`
}

type RemoteSessionCollaboratorInput struct {
	Username string `json:"username"`
	Access   string `json:"access"`
}

type RemoteSession struct {
	ID                     string                      `json:"id"`
	AssetID                string                      `json:"assetId"`
	AssetName              string                      `json:"assetName"`
	IP                     string                      `json:"ip"`
	Port                   int                         `json:"port"`
	CredentialID           string                      `json:"credentialId"`
	Operator               string                      `json:"operator"`
	Roles                  []string                    `json:"roles"`
	Collaborators          []RemoteSessionCollaborator `json:"collaborators"`
	Status                 string                      `json:"status"`
	CreatedAt              string                      `json:"createdAt"`
	ExpiresAt              string                      `json:"expiresAt"`
	RecordingRetentionDays int                         `json:"recordingRetentionDays,omitempty"`
	ClosedAt               string                      `json:"closedAt,omitempty"`
	ArchiveStatus          string                      `json:"archiveStatus,omitempty"`
	ArchiveBucket          string                      `json:"archiveBucket,omitempty"`
	ArchiveKey             string                      `json:"archiveKey,omitempty"`
	ArchiveSHA256          string                      `json:"archiveSha256,omitempty"`
	ArchiveSize            int64                       `json:"archiveSize,omitempty"`
	ArchivedAt             string                      `json:"archivedAt,omitempty"`
	ArchiveError           string                      `json:"archiveError,omitempty"`
	ArchiveDeletedAt       string                      `json:"archiveDeletedAt,omitempty"`
	ArchiveDeleteError     string                      `json:"archiveDeleteError,omitempty"`
	OwnerNode              string                      `json:"ownerNode,omitempty"`
	LeaseHeartbeatAt       string                      `json:"leaseHeartbeatAt,omitempty"`
	LeaseExpiresAt         string                      `json:"leaseExpiresAt,omitempty"`
	Logs                   []RemoteSessionLog          `json:"logs"`
	AccessMode             string                      `json:"accessMode,omitempty"`
	ActiveConnections      int                         `json:"activeConnections"`
	ControllerOnline       bool                        `json:"controllerOnline"`
	CurrentUser            string                      `json:"-"`
	CurrentRoles           []string                    `json:"-"`
}

func remoteGrantID() string {
	data := make([]byte, 8)
	_, _ = rand.Read(data)
	return "grant-" + hex.EncodeToString(data)
}
func remoteHostKeyID() string {
	data := make([]byte, 8)
	_, _ = rand.Read(data)
	return "hostkey-" + hex.EncodeToString(data)
}
func remoteSessionID() string {
	data := make([]byte, 10)
	_, _ = rand.Read(data)
	return "rses-" + hex.EncodeToString(data)
}
func (s *Service) AccessGrants() []AccessGrant {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AccessGrant, 0, len(s.accessGrants))
	for _, item := range s.accessGrants {
		out = append(out, cloneAccessGrant(item))
	}
	return out
}

func (s *Service) CreateAccessGrant(input AccessGrantInput, createdBy string) (AccessGrant, error) {
	item, err := normalizeAccessGrant(input)
	if err != nil {
		return AccessGrant{}, err
	}
	item.ID, item.CreatedBy = remoteGrantID(), createdBy
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accessGrants = append([]AccessGrant{item}, s.accessGrants...)
	if s.db != nil {
		if err = s.persistAccessGrant(item); err != nil {
			return AccessGrant{}, err
		}
	}
	return cloneAccessGrant(item), nil
}
func (s *Service) ToggleAccessGrant(id string) (AccessGrant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.accessGrants {
		if s.accessGrants[i].ID == id {
			s.accessGrants[i].Enabled = !s.accessGrants[i].Enabled
			if s.db != nil {
				_ = s.persistAccessGrant(s.accessGrants[i])
			}
			return cloneAccessGrant(s.accessGrants[i]), nil
		}
	}
	return AccessGrant{}, ErrNotFound
}
func (s *Service) DeleteAccessGrant(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.accessGrants {
		if s.accessGrants[i].ID == id {
			s.accessGrants = append(s.accessGrants[:i], s.accessGrants[i+1:]...)
			if s.db != nil {
				_ = s.deleteAccessGrantRow(id)
			}
			return nil
		}
	}
	return ErrNotFound
}
func normalizeAccessGrant(input AccessGrantInput) (AccessGrant, error) {
	input.SubjectType, input.Subject = strings.ToLower(strings.TrimSpace(input.SubjectType)), strings.TrimSpace(input.Subject)
	assetIDs := normalizeGrantValues(append([]string{input.AssetID}, input.AssetIDs...))
	projectGroups := normalizeGrantValues(append([]string{input.ProjectGroup}, input.ProjectGroups...))
	tags := normalizeGrantValues(input.Tags)
	if (input.SubjectType != "user" && input.SubjectType != "role") || input.Subject == "" || (len(assetIDs) == 0 && len(projectGroups) == 0 && len(tags) == 0) {
		return AccessGrant{}, ErrRemoteValidation
	}
	permissions := []string{}
	for _, permission := range input.Permissions {
		permission = strings.ToLower(strings.TrimSpace(permission))
		if permission == "*" {
			permissions = []string{"terminal", "file"}
			break
		}
		if (permission == "terminal" || permission == "file") && !remoteContainsString(permissions, permission) {
			permissions = append(permissions, permission)
		}
	}
	if len(permissions) == 0 {
		return AccessGrant{}, ErrRemoteValidation
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	item := AccessGrant{SubjectType: input.SubjectType, Subject: input.Subject, AssetIDs: assetIDs, ProjectGroups: projectGroups, Tags: tags, Permissions: permissions, Enabled: enabled}
	if len(assetIDs) > 0 {
		item.AssetID = assetIDs[0]
	}
	if len(projectGroups) > 0 {
		item.ProjectGroup = projectGroups[0]
	}
	return item, nil
}

func normalizeGrantValues(values []string) []string {
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 128 || remoteContainsString(out, value) {
			continue
		}
		out = append(out, value)
		if len(out) >= 100 {
			break
		}
	}
	return out
}

func (s *Service) authorizeRemoteAccess(username string, roles []string, assetID, projectGroup, permission string) bool {
	return s.authorizeRemoteAccessScoped(username, roles, assetID, projectGroup, nil, permission)
}

func (s *Service) authorizeRemoteAccessScoped(username string, roles []string, assetID, projectGroup string, tags []string, permission string) bool {
	for _, role := range roles {
		if role == "admin" || role == "platform-admin" {
			return true
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, grant := range s.accessGrants {
		if !grant.Enabled {
			continue
		}
		match := (grant.SubjectType == "user" && grant.Subject == username) || (grant.SubjectType == "role" && remoteContainsString(roles, grant.Subject))
		if !match || !accessGrantMatchesScope(grant, assetID, projectGroup, tags) {
			continue
		}
		if remoteContainsString(grant.Permissions, permission) {
			return true
		}
	}
	return false
}

func accessGrantMatchesScope(grant AccessGrant, assetID, projectGroup string, tags []string) bool {
	assetIDs := normalizeGrantValues(append([]string{grant.AssetID}, grant.AssetIDs...))
	projectGroups := normalizeGrantValues(append([]string{grant.ProjectGroup}, grant.ProjectGroups...))
	if len(assetIDs) > 0 && !remoteContainsString(assetIDs, assetID) {
		return false
	}
	if len(projectGroups) > 0 && !remoteContainsString(projectGroups, projectGroup) {
		return false
	}
	if len(grant.Tags) > 0 {
		matched := false
		for _, wanted := range grant.Tags {
			if remoteContainsString(tags, wanted) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func (s *Service) authorizeRemoteAccessForAsset(username string, roles []string, assetID, permission string) bool {
	if s.cmdb == nil {
		return false
	}
	asset, err := s.cmdb.GetAsset(assetID)
	if err != nil {
		return false
	}
	return s.authorizeRemoteAccessScoped(username, roles, asset.ID, asset.ProjectGroup, asset.Tags, permission)
}

func cloneAccessGrant(item AccessGrant) AccessGrant {
	item.AssetIDs = append([]string(nil), item.AssetIDs...)
	item.ProjectGroups = append([]string(nil), item.ProjectGroups...)
	item.Tags = append([]string(nil), item.Tags...)
	item.Permissions = append([]string(nil), item.Permissions...)
	return item
}
func remoteContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func (s *Service) RemoteSessions(username string, roles []string) []RemoteSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	out := []RemoteSession{}
	for i := range s.remoteSessions {
		if s.remoteSessions[i].Status == "closed" || parseRemoteTime(s.remoteSessions[i].ExpiresAt).Before(now) {
			continue
		}
		access := remoteSessionAccess(s.remoteSessions[i], username, roles)
		if access == "" {
			continue
		}
		item := cloneRemoteSession(s.remoteSessions[i])
		item.AccessMode = access
		item.CurrentUser = username
		item.CurrentRoles = append([]string(nil), roles...)
		s.populateRemoteSessionRuntime(&item)
		out = append(out, item)
	}
	return out
}
func (s *Service) CreateRemoteSession(assetID, credentialID, operator string, roles []string) (RemoteSession, error) {
	if s.cmdb == nil {
		return RemoteSession{}, ErrRemoteValidation
	}
	asset, err := s.cmdb.GetAsset(strings.TrimSpace(assetID))
	if err != nil || asset.IP == "" {
		return RemoteSession{}, ErrRemoteValidation
	}
	if !s.authorizeRemoteAccessScoped(operator, roles, asset.ID, asset.ProjectGroup, asset.Tags, "terminal") {
		return RemoteSession{}, ErrRemoteValidation
	}
	credentialID = strings.TrimSpace(credentialID)
	if _, _, err = s.resolveRemoteCredential(credentialID); err != nil {
		return RemoteSession{}, err
	}
	now := time.Now()
	policy := s.EffectiveRemoteSecurityPolicy(operator, roles)
	s.mu.Lock()
	activeSessions := 0
	for i := range s.remoteSessions {
		if s.remoteSessions[i].Operator == operator && s.remoteSessions[i].Status == "active" && !parseRemoteTime(s.remoteSessions[i].ExpiresAt).Before(now) {
			activeSessions++
		}
	}
	if activeSessions >= policy.MaxConcurrentSessions {
		s.mu.Unlock()
		return RemoteSession{}, fmt.Errorf("%w: concurrent remote session limit reached (%d)", ErrRemoteValidation, policy.MaxConcurrentSessions)
	}
	item := RemoteSession{ID: remoteSessionID(), AssetID: asset.ID, AssetName: asset.Name, IP: asset.IP, Port: 22, CredentialID: credentialID, Operator: operator, Roles: append([]string(nil), roles...), Collaborators: []RemoteSessionCollaborator{}, Status: "active", CreatedAt: now.Format("2006-01-02 15:04:05"), ExpiresAt: now.Add(time.Duration(policy.MaxSessionMinutes) * time.Minute).Format("2006-01-02 15:04:05"), RecordingRetentionDays: policy.RecordingRetentionDays, OwnerNode: "", LeaseHeartbeatAt: "", LeaseExpiresAt: "", Logs: []RemoteSessionLog{{Time: now.Format("15:04:05"), Level: "success", Kind: "session", Message: "远程会话已建立"}}}
	s.remoteSessions = append([]RemoteSession{item}, s.remoteSessions...)
	s.mu.Unlock()
	if s.db != nil {
		_ = s.persistRemoteSession(item)
	}
	s.scheduleRemoteSessionExpiry(item.ID, parseRemoteTime(item.ExpiresAt))
	return cloneRemoteSession(item), nil
}

func (s *Service) scheduleRemoteSessionExpiry(sessionID string, expiresAt time.Time) {
	delay := time.Until(expiresAt)
	if delay <= 0 {
		return
	}
	time.AfterFunc(delay, func() {
		now := time.Now()
		s.mu.Lock()
		var expired RemoteSession
		for i := range s.remoteSessions {
			if s.remoteSessions[i].ID != sessionID || s.remoteSessions[i].Status != "active" {
				continue
			}
			s.remoteSessions[i].Status = "interrupted"
			s.remoteSessions[i].ClosedAt = now.Format("2006-01-02 15:04:05")
			s.remoteSessions[i].Logs = append(s.remoteSessions[i].Logs, RemoteSessionLog{Time: now.Format("15:04:05"), Level: "error", Kind: "session", Message: "会话已达到安全策略时长上限，自动关闭"})
			expired = cloneRemoteSession(s.remoteSessions[i])
			break
		}
		s.mu.Unlock()
		if expired.ID == "" {
			return
		}
		if s.db != nil {
			_ = s.persistRemoteSession(expired)
		}
		s.CloseRemoteTerminal(sessionID)
	})
}

func (s *Service) ExecuteRemoteSession(sessionID, command, operator string, roles []string) (RemoteSession, error) {
	command = strings.TrimSpace(command)
	if command == "" || len(command) > 4000 {
		return RemoteSession{}, ErrRemoteValidation
	}
	item, _, err := s.sessionForOperator(sessionID, operator, roles)
	if err != nil {
		return RemoteSession{}, err
	}
	if item.AccessMode != "control" {
		return RemoteSession{}, ErrRemoteForbidden
	}
	if err = s.RenewRemoteSessionLease(context.Background(), sessionID); err != nil {
		return RemoteSession{}, err
	}
	username, secret, err := s.resolveRemoteCredential(item.CredentialID)
	if err != nil {
		return RemoteSession{}, err
	}
	started := time.Now()
	output, runErr := s.runTrustedSSH(item.AssetID, item.IP, item.Port, command, username, secret, 30*time.Second)
	message, level := truncateOutput(output), "success"
	if runErr != nil {
		level = "error"
		if message != "" {
			message += "\n" + runErr.Error()
		} else {
			message = runErr.Error()
		}
	}
	duration := time.Since(started)
	result, ok := s.appendSessionLog(item.ID, level, "command", "$ "+command+"\n"+message+"\n("+duration.Round(time.Millisecond).String()+")", duration)
	if !ok {
		return RemoteSession{}, ErrNotFound
	}
	return result, runErr
}

func (s *Service) UploadRemoteFile(sessionID, remotePath string, reader io.Reader, size int64, operator string, roles []string) error {
	if size <= 0 {
		return ErrRemoteValidation
	}
	item, _, err := s.sessionForOperator(sessionID, operator, roles)
	if err != nil {
		return err
	}
	if item.AccessMode != "control" {
		return ErrRemoteForbidden
	}
	if err = s.RenewRemoteSessionLease(context.Background(), sessionID); err != nil {
		return err
	}
	if !s.authorizeRemoteAccessForAsset(operator, roles, item.AssetID, "file") {
		return ErrRemoteValidation
	}
	if err = validateRemotePath(remotePath); err != nil {
		return err
	}
	if err = s.checkRemoteFilePolicy(item, operator, roles, remotePath, true, size); err != nil {
		return err
	}
	username, secret, err := s.resolveRemoteCredential(item.CredentialID)
	if err != nil {
		return err
	}
	client, err := s.dialTrusted(item.AssetID, item.IP, item.Port, username, secret)
	if err != nil {
		return err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	session.Stdin = reader
	parent := path.Dir(remotePath)
	command := "cat > " + shellQuote(remotePath)
	if parent != "." && parent != "/" {
		command = "mkdir -p " + shellQuote(parent) + " && " + command
	}
	if _, err = session.CombinedOutput(command); err != nil {
		return err
	}
	s.appendSessionLog(sessionID, "success", "file-upload", "上传文件: "+remotePath, 0)
	return nil
}
func (s *Service) DownloadRemoteFile(sessionID, remotePath, operator string, roles []string) ([]byte, error) {
	if err := validateRemotePath(remotePath); err != nil {
		return nil, err
	}
	item, _, err := s.sessionForOperator(sessionID, operator, roles)
	if err != nil {
		return nil, err
	}
	if item.AccessMode != "control" {
		return nil, ErrRemoteForbidden
	}
	if err = s.RenewRemoteSessionLease(context.Background(), sessionID); err != nil {
		return nil, err
	}
	if !s.authorizeRemoteAccessForAsset(operator, roles, item.AssetID, "file") {
		return nil, ErrRemoteValidation
	}
	if err = s.checkRemoteFilePolicy(item, operator, roles, remotePath, false, 0); err != nil {
		return nil, err
	}
	username, secret, err := s.resolveRemoteCredential(item.CredentialID)
	if err != nil {
		return nil, err
	}
	client, err := s.dialTrusted(item.AssetID, item.IP, item.Port, username, secret)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer session.Close()
	output, err := session.Output("cat " + shellQuote(remotePath))
	if err != nil {
		return nil, err
	}
	policy := s.EffectiveRemoteSecurityPolicy(operator, roles)
	if int64(len(output)) > int64(policy.DownloadMaxMB)<<20 {
		return nil, fmt.Errorf("%w: download exceeds policy limit %dMB", ErrRemoteValidation, policy.DownloadMaxMB)
	}
	s.appendSessionLog(sessionID, "success", "file-download", "下载文件: "+remotePath, 0)
	return output, nil
}
func (s *Service) CloseRemoteSession(sessionID, operator string, roles []string) error {
	if _, _, err := s.sessionManagerFor(sessionID, operator, roles); err != nil {
		return err
	}
	now := time.Now()
	s.mu.Lock()
	var closed RemoteSession
	found := false
	for i := range s.remoteSessions {
		if s.remoteSessions[i].ID != sessionID || s.remoteSessions[i].Status != "active" {
			continue
		}
		s.remoteSessions[i].Status = "closed"
		s.remoteSessions[i].ClosedAt = now.Format("2006-01-02 15:04:05")
		s.remoteSessions[i].Logs = append(s.remoteSessions[i].Logs, RemoteSessionLog{Time: now.Format("15:04:05"), Level: "success", Kind: "session", Message: "远程会话已关闭"})
		closed = cloneRemoteSession(s.remoteSessions[i])
		found = true
		break
	}
	s.mu.Unlock()
	if !found {
		return ErrNotFound
	}
	if s.db != nil {
		_ = s.persistRemoteSession(closed)
	}
	s.CloseRemoteTerminal(sessionID)
	s.scheduleTerminalArchive(sessionID)
	return nil
}

func (s *Service) ForceDisconnectRemoteSession(sessionID, operator string, roles []string) error {
	if !remoteContainsString(roles, "admin") && !remoteContainsString(roles, "platform-admin") {
		return ErrRemoteForbidden
	}
	now := time.Now()
	s.mu.Lock()
	var closed RemoteSession
	found := false
	for i := range s.remoteSessions {
		if s.remoteSessions[i].ID != sessionID || s.remoteSessions[i].Status != "active" {
			continue
		}
		s.remoteSessions[i].Status = "closed"
		s.remoteSessions[i].ClosedAt = now.Format("2006-01-02 15:04:05")
		s.remoteSessions[i].Logs = append(s.remoteSessions[i].Logs, RemoteSessionLog{Time: now.Format("15:04:05"), Level: "error", Kind: "session", Message: "管理员 " + operator + " 强制断开远程会话"})
		closed = cloneRemoteSession(s.remoteSessions[i])
		found = true
		break
	}
	s.mu.Unlock()
	if !found {
		return ErrNotFound
	}
	if s.db != nil {
		_ = s.persistRemoteSession(closed)
	}
	s.CloseRemoteTerminal(sessionID)
	s.scheduleTerminalArchive(sessionID)
	return nil
}

func (s *Service) RemoteSessionCollaborators(sessionID, operator string, roles []string) ([]RemoteSessionCollaborator, error) {
	item, _, err := s.sessionForOperator(sessionID, operator, roles)
	if err != nil {
		return nil, err
	}
	return append([]RemoteSessionCollaborator(nil), item.Collaborators...), nil
}

func (s *Service) AddRemoteSessionCollaborator(sessionID string, input RemoteSessionCollaboratorInput, operator string, roles []string) ([]RemoteSessionCollaborator, error) {
	username := strings.ToLower(strings.TrimSpace(input.Username))
	access := strings.ToLower(strings.TrimSpace(input.Access))
	if access == "read" {
		access = "readonly"
	}
	if username == "" || (access != "control" && access != "readonly") {
		return nil, ErrRemoteValidation
	}
	item, _, err := s.sessionManagerFor(sessionID, operator, roles)
	if err != nil {
		return nil, err
	}
	if item.Operator == username {
		return nil, ErrRemoteValidation
	}
	if access == "control" && !s.EffectiveRemoteSecurityPolicy(operator, roles).AllowControlCollaborators {
		return nil, fmt.Errorf("%w: control collaborators are disabled by policy", ErrRemoteValidation)
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	s.mu.Lock()
	var updated RemoteSession
	for i := range s.remoteSessions {
		if s.remoteSessions[i].ID != sessionID {
			continue
		}
		for _, collaborator := range s.remoteSessions[i].Collaborators {
			if collaborator.Username == username {
				s.mu.Unlock()
				return nil, ErrRemoteValidation
			}
		}
		s.remoteSessions[i].Collaborators = append(s.remoteSessions[i].Collaborators, RemoteSessionCollaborator{Username: username, Access: access, AddedBy: operator, AddedAt: now})
		label := "可控"
		if access == "readonly" {
			label = "只读"
		}
		s.remoteSessions[i].Logs = append(s.remoteSessions[i].Logs, RemoteSessionLog{Time: time.Now().Format("15:04:05"), Level: "success", Kind: "collaborator", Message: "添加协作者 " + username + "（" + label + "）"})
		updated = cloneRemoteSession(s.remoteSessions[i])
		break
	}
	s.mu.Unlock()
	if updated.ID == "" {
		return nil, ErrNotFound
	}
	if s.db != nil {
		_ = s.persistRemoteSession(updated)
	}
	return append([]RemoteSessionCollaborator(nil), updated.Collaborators...), nil
}

func (s *Service) RemoveRemoteSessionCollaborator(sessionID, username, operator string, roles []string) ([]RemoteSessionCollaborator, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return nil, ErrRemoteValidation
	}
	if _, _, err := s.sessionManagerFor(sessionID, operator, roles); err != nil {
		return nil, err
	}
	s.mu.Lock()
	var updated RemoteSession
	for i := range s.remoteSessions {
		if s.remoteSessions[i].ID != sessionID {
			continue
		}
		collaborators := s.remoteSessions[i].Collaborators[:0]
		removed := false
		for _, collaborator := range s.remoteSessions[i].Collaborators {
			if collaborator.Username == username {
				removed = true
				continue
			}
			collaborators = append(collaborators, collaborator)
		}
		if !removed {
			s.mu.Unlock()
			return nil, ErrNotFound
		}
		s.remoteSessions[i].Collaborators = collaborators
		s.remoteSessions[i].Logs = append(s.remoteSessions[i].Logs, RemoteSessionLog{Time: time.Now().Format("15:04:05"), Level: "success", Kind: "collaborator", Message: "移除协作者 " + username})
		updated = cloneRemoteSession(s.remoteSessions[i])
		break
	}
	s.mu.Unlock()
	if updated.ID == "" {
		return nil, ErrNotFound
	}
	if s.db != nil {
		_ = s.persistRemoteSession(updated)
	}
	return append([]RemoteSessionCollaborator(nil), updated.Collaborators...), nil
}

func (s *Service) ExportTerminalRecording(sessionID, operator string, roles []string) ([]byte, error) {
	recording, err := s.TerminalRecording(sessionID, operator, roles)
	if err != nil {
		return nil, err
	}
	var output strings.Builder
	output.WriteString("CMDB remote terminal recording\nSession: " + sessionID + "\n\n")
	for _, event := range recording.Events {
		decoded, decodeErr := base64.StdEncoding.DecodeString(event.Data)
		if decodeErr != nil {
			continue
		}
		output.WriteString("[" + event.CreatedAt + "] [" + event.Direction + "]\n")
		output.Write(decoded)
		if len(decoded) > 0 && decoded[len(decoded)-1] != '\n' {
			output.WriteByte('\n')
		}
	}
	return []byte(output.String()), nil
}

func (s *Service) ExportCommandLog(sessionID, operator string, roles []string) ([]byte, error) {
	item, err := s.RemoteSessionReplay(sessionID, operator, roles)
	if err != nil {
		return nil, err
	}
	var output strings.Builder
	output.WriteString("CMDB remote session command log\nSession: " + item.ID + "\nAsset: " + item.AssetName + "/" + item.IP + "\nOperator: " + item.Operator + "\n\n")
	for _, entry := range item.Logs {
		if entry.Kind != "command" && entry.Kind != "interactive-command" && entry.Kind != "risk-blocked" && entry.Kind != "command-approved" && entry.Kind != "command-rejected" {
			continue
		}
		output.WriteString("[" + entry.Time + "] [" + entry.Level + "] [" + entry.Kind + "] " + entry.Message + "\n")
	}
	return []byte(output.String()), nil
}

func (s *Service) sessionForOperator(id, operator string, roles []string) (RemoteSession, int, error) {
	item, index, err := s.findRemoteSessionForOperator(id, operator, roles)
	if err == nil || !errors.Is(err, ErrNotFound) || s.db == nil {
		return item, index, err
	}
	if syncErr := s.syncRemoteSessionsFromDB(); syncErr != nil {
		return RemoteSession{}, -1, syncErr
	}
	return s.findRemoteSessionForOperator(id, operator, roles)
}

func (s *Service) findRemoteSessionForOperator(id, operator string, roles []string) (RemoteSession, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i, item := range s.remoteSessions {
		if item.ID == id {
			if item.Status != "active" || parseRemoteTime(item.ExpiresAt).Before(time.Now()) {
				return RemoteSession{}, -1, ErrRemoteValidation
			}
			access := remoteSessionAccess(item, operator, roles)
			if access == "" {
				return RemoteSession{}, -1, ErrRemoteForbidden
			}
			item = cloneRemoteSession(item)
			item.AccessMode = access
			item.CurrentUser = operator
			item.CurrentRoles = append([]string(nil), roles...)
			s.populateRemoteSessionRuntime(&item)
			return item, i, nil
		}
	}
	return RemoteSession{}, -1, ErrNotFound
}

func (s *Service) sessionManagerFor(id, operator string, roles []string) (RemoteSession, int, error) {
	item, index, err := s.sessionForOperator(id, operator, roles)
	if err != nil {
		return RemoteSession{}, -1, err
	}
	if item.Operator != operator && !remoteContainsString(roles, "admin") && !remoteContainsString(roles, "platform-admin") {
		return RemoteSession{}, -1, ErrRemoteForbidden
	}
	return item, index, nil
}

func remoteSessionAccess(item RemoteSession, operator string, roles []string) string {
	if item.Operator == operator || remoteContainsString(roles, "admin") || remoteContainsString(roles, "platform-admin") {
		return "control"
	}
	for _, collaborator := range item.Collaborators {
		if collaborator.Username == operator {
			if collaborator.Access == "readonly" {
				return "readonly"
			}
			return "control"
		}
	}
	return ""
}

func (s *Service) populateRemoteSessionRuntime(item *RemoteSession) {
	if channel := s.terminals[item.ID]; channel != nil {
		item.ActiveConnections, item.ControllerOnline = channel.Presence()
	}
}
func (s *Service) appendSessionLog(id, level, kind, message string, duration time.Duration) (RemoteSession, bool) {
	s.mu.Lock()
	var updated RemoteSession
	found := false
	for i := range s.remoteSessions {
		if s.remoteSessions[i].ID != id {
			continue
		}
		entry := RemoteSessionLog{Time: time.Now().Format("15:04:05"), Level: level, Kind: kind, Message: message, DurationMS: duration.Milliseconds()}
		if len(s.remoteSessions[i].Logs) >= 1000 {
			s.remoteSessions[i].Logs = append(s.remoteSessions[i].Logs[len(s.remoteSessions[i].Logs)-999:], entry)
		} else {
			s.remoteSessions[i].Logs = append(s.remoteSessions[i].Logs, entry)
		}
		updated = cloneRemoteSession(s.remoteSessions[i])
		found = true
		break
	}
	s.mu.Unlock()
	if !found {
		return RemoteSession{}, false
	}
	if s.db != nil {
		_ = s.persistRemoteSession(updated)
	}
	return updated, true
}

func (s *Service) dialTrusted(assetID, ip string, port int, username, secret string) (*ssh.Client, error) {
	return ssh.Dial("tcp", net.JoinHostPort(ip, strconv.Itoa(port)), &ssh.ClientConfig{User: username, Auth: remoteAuthMethods(secret), HostKeyCallback: s.trustedHostKeyCallback(assetID, ip, port), Timeout: 10 * time.Second})
}
func (s *Service) runTrustedSSH(assetID, ip string, port int, command, username, secret string, timeout time.Duration) (string, error) {
	client, err := s.dialTrusted(assetID, ip, port, username, secret)
	if err != nil {
		return "", err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	output, err := session.CombinedOutput(command)
	return string(output), err
}
func cloneRemoteSession(item RemoteSession) RemoteSession {
	item.Roles = append([]string(nil), item.Roles...)
	item.Collaborators = append([]RemoteSessionCollaborator(nil), item.Collaborators...)
	item.Logs = append([]RemoteSessionLog(nil), item.Logs...)
	if item.Roles == nil {
		item.Roles = []string{}
	}
	if item.Collaborators == nil {
		item.Collaborators = []RemoteSessionCollaborator{}
	}
	if item.Logs == nil {
		item.Logs = []RemoteSessionLog{}
	}
	item.CurrentRoles = append([]string(nil), item.CurrentRoles...)
	return item
}
func parseRemoteTime(value string) time.Time {
	parsed, _ := time.ParseInLocation("2006-01-02 15:04:05", value, time.Local)
	return parsed
}
func dialRemote(ip string, port int, username, secret string) (*ssh.Client, error) {
	callback, err := sshHostKeyCallback()
	if err != nil {
		return nil, err
	}
	return ssh.Dial("tcp", net.JoinHostPort(ip, strconv.Itoa(port)), &ssh.ClientConfig{User: username, Auth: remoteAuthMethods(secret), HostKeyCallback: callback, Timeout: 10 * time.Second})
}

func remoteAuthMethods(secret string) []ssh.AuthMethod {
	if strings.Contains(secret, "PRIVATE KEY") {
		if signer, err := ssh.ParsePrivateKey([]byte(secret)); err == nil {
			return []ssh.AuthMethod{ssh.PublicKeys(signer)}
		}
	}
	return []ssh.AuthMethod{ssh.Password(secret)}
}
func validateRemotePath(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 1024 || strings.Contains(value, "..") || strings.ContainsRune(value, 0) {
		return ErrRemoteValidation
	}
	return nil
}
func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func (s *Service) loadAccessGrants() error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.Query(`SELECT id,subject_type,subject,asset_id,project_group,asset_ids,project_groups,tags,permissions,enabled,created_by FROM remote_access_grants ORDER BY updated_at DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []AccessGrant{}
	for rows.Next() {
		var item AccessGrant
		var assetIDs, projectGroups, tags, permissions []byte
		if err = rows.Scan(&item.ID, &item.SubjectType, &item.Subject, &item.AssetID, &item.ProjectGroup, &assetIDs, &projectGroups, &tags, &permissions, &item.Enabled, &item.CreatedBy); err != nil {
			return err
		}
		_ = json.Unmarshal(assetIDs, &item.AssetIDs)
		_ = json.Unmarshal(projectGroups, &item.ProjectGroups)
		_ = json.Unmarshal(tags, &item.Tags)
		if json.Unmarshal(permissions, &item.Permissions) != nil {
			item.Permissions = []string{}
		}
		if item.AssetID != "" && !remoteContainsString(item.AssetIDs, item.AssetID) {
			item.AssetIDs = append([]string{item.AssetID}, item.AssetIDs...)
		}
		if item.ProjectGroup != "" && !remoteContainsString(item.ProjectGroups, item.ProjectGroup) {
			item.ProjectGroups = append([]string{item.ProjectGroup}, item.ProjectGroups...)
		}
		out = append(out, cloneAccessGrant(item))
	}
	s.accessGrants = out
	return rows.Err()
}
func (s *Service) persistAccessGrant(item AccessGrant) error {
	if s.db == nil {
		return nil
	}
	assetIDs, _ := json.Marshal(item.AssetIDs)
	projectGroups, _ := json.Marshal(item.ProjectGroups)
	tags, _ := json.Marshal(item.Tags)
	permissions, _ := json.Marshal(item.Permissions)
	_, err := s.db.Exec(`INSERT INTO remote_access_grants(id,subject_type,subject,asset_id,project_group,asset_ids,project_groups,tags,permissions,enabled,created_by,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,now()) ON CONFLICT(id) DO UPDATE SET subject_type=excluded.subject_type,subject=excluded.subject,asset_id=excluded.asset_id,project_group=excluded.project_group,asset_ids=excluded.asset_ids,project_groups=excluded.project_groups,tags=excluded.tags,permissions=excluded.permissions,enabled=excluded.enabled,updated_at=now()`, item.ID, item.SubjectType, item.Subject, item.AssetID, item.ProjectGroup, assetIDs, projectGroups, tags, permissions, item.Enabled, item.CreatedBy)
	return err
}

func (s *Service) deleteAccessGrantRow(id string) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM remote_access_grants WHERE id=$1`, id)
	return err
}

func (s *Service) RemoteHostKeys() []RemoteHostKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]RemoteHostKey(nil), s.hostKeys...)
}
func (s *Service) ProbeRemoteHostKey(assetID, credentialID, operator string, roles []string) (RemoteHostKeyProbe, error) {
	if s.cmdb == nil {
		return RemoteHostKeyProbe{}, ErrRemoteValidation
	}
	asset, err := s.cmdb.GetAsset(strings.TrimSpace(assetID))
	if err != nil || asset.IP == "" {
		return RemoteHostKeyProbe{}, ErrRemoteValidation
	}
	if !s.authorizeRemoteAccessScoped(operator, roles, asset.ID, asset.ProjectGroup, asset.Tags, "terminal") {
		return RemoteHostKeyProbe{}, ErrRemoteValidation
	}
	username, secret, err := s.resolveRemoteCredential(strings.TrimSpace(credentialID))
	if err != nil {
		return RemoteHostKeyProbe{}, err
	}
	var captured ssh.PublicKey
	config := &ssh.ClientConfig{User: username, Auth: remoteAuthMethods(secret), HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error { captured = key; return nil }, Timeout: 10 * time.Second}
	client, err := ssh.Dial("tcp", net.JoinHostPort(asset.IP, "22"), config)
	if err != nil {
		return RemoteHostKeyProbe{}, err
	}
	_ = client.Close()
	if captured == nil {
		return RemoteHostKeyProbe{}, fmt.Errorf("failed to capture SSH host key")
	}
	fingerprint := ssh.FingerprintSHA256(captured)
	probe := RemoteHostKeyProbe{AssetID: asset.ID, Host: asset.IP, Port: 22, KeyType: captured.Type(), Fingerprint: fingerprint, PublicKey: base64.StdEncoding.EncodeToString(captured.Marshal())}
	s.mu.RLock()
	for _, item := range s.hostKeys {
		if item.AssetID == asset.ID && item.Host == asset.IP && item.Port == 22 {
			probe.TrustedFingerprint = item.Fingerprint
			probe.Trusted = item.Fingerprint == fingerprint
			probe.Changed = !probe.Trusted
			break
		}
	}
	s.mu.RUnlock()
	return probe, nil
}
func (s *Service) TrustRemoteHostKey(input RemoteHostKeyInput, addedBy string) (RemoteHostKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(input.PublicKey))
	if err != nil {
		return RemoteHostKey{}, ErrRemoteValidation
	}
	key, err := ssh.ParsePublicKey(raw)
	if err != nil {
		return RemoteHostKey{}, ErrRemoteValidation
	}
	fingerprint := ssh.FingerprintSHA256(key)
	if strings.TrimSpace(input.Fingerprint) != fingerprint {
		return RemoteHostKey{}, ErrRemoteValidation
	}
	input.AssetID, input.Host = strings.TrimSpace(input.AssetID), strings.TrimSpace(input.Host)
	if input.AssetID == "" || input.Host == "" {
		return RemoteHostKey{}, ErrRemoteValidation
	}
	if input.Port == 0 {
		input.Port = 22
	}
	item := RemoteHostKey{ID: remoteHostKeyID(), AssetID: input.AssetID, Host: input.Host, Port: input.Port, KeyType: key.Type(), Fingerprint: fingerprint, PublicKey: base64.StdEncoding.EncodeToString(key.Marshal()), AddedBy: addedBy, CreatedAt: time.Now().Format("2006-01-02 15:04:05")}
	s.mu.Lock()
	replaced := false
	for i := range s.hostKeys {
		if s.hostKeys[i].AssetID == item.AssetID && s.hostKeys[i].Host == item.Host && s.hostKeys[i].Port == item.Port {
			item.ID, item.CreatedAt = s.hostKeys[i].ID, s.hostKeys[i].CreatedAt
			s.hostKeys[i] = item
			replaced = true
			break
		}
	}
	if !replaced {
		s.hostKeys = append([]RemoteHostKey{item}, s.hostKeys...)
	}
	s.mu.Unlock()
	if s.db != nil {
		_ = s.persistHostKey(item)
	}
	return item, nil
}
func (s *Service) DeleteRemoteHostKey(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.hostKeys {
		if s.hostKeys[i].ID == id {
			s.hostKeys = append(s.hostKeys[:i], s.hostKeys[i+1:]...)
			if s.db != nil {
				_ = s.deleteHostKeyRow(id)
			}
			return nil
		}
	}
	return ErrNotFound
}
func (s *Service) findTrustedHostKeyLocked(assetID, host string, port int, fingerprint string) bool {
	for _, item := range s.hostKeys {
		if item.AssetID == assetID && item.Host == host && item.Port == port && item.Fingerprint == fingerprint {
			return true
		}
	}
	return false
}
func (s *Service) trustedHostKeyCallback(assetID, host string, port int) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		fingerprint := ssh.FingerprintSHA256(key)
		s.mu.RLock()
		trusted := s.findTrustedHostKeyLocked(assetID, host, port, fingerprint)
		s.mu.RUnlock()
		if trusted {
			return nil
		}
		callback, err := sshHostKeyCallback()
		if err == nil {
			return callback(hostname, remote, key)
		}
		return fmt.Errorf("SSH 主机指纹未信任，请先在远程运维页面探测并信任指纹")
	}
}
func (s *Service) loadHostKeys() error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.Query(`SELECT id,asset_id,host,port,key_type,fingerprint,public_key,added_by,created_at FROM remote_host_keys ORDER BY updated_at DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []RemoteHostKey{}
	for rows.Next() {
		var item RemoteHostKey
		var created time.Time
		if err = rows.Scan(&item.ID, &item.AssetID, &item.Host, &item.Port, &item.KeyType, &item.Fingerprint, &item.PublicKey, &item.AddedBy, &created); err != nil {
			return err
		}
		item.CreatedAt = created.Local().Format("2006-01-02 15:04:05")
		out = append(out, item)
	}
	s.hostKeys = out
	return rows.Err()
}
func (s *Service) persistHostKey(item RemoteHostKey) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO remote_host_keys(id,asset_id,host,port,key_type,fingerprint,public_key,added_by,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,now()) ON CONFLICT(id) DO UPDATE SET asset_id=excluded.asset_id,host=excluded.host,port=excluded.port,key_type=excluded.key_type,fingerprint=excluded.fingerprint,public_key=excluded.public_key,updated_at=now()`, item.ID, item.AssetID, item.Host, item.Port, item.KeyType, item.Fingerprint, item.PublicKey, item.AddedBy)
	return err
}
func (s *Service) deleteHostKeyRow(id string) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM remote_host_keys WHERE id=$1`, id)
	return err
}

func (s *Service) RemoteSessionHistory(username string, roles []string) []RemoteSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []RemoteSession{}
	for i := 0; i < len(s.remoteSessions) && len(out) < 200; i++ {
		access := remoteSessionAccess(s.remoteSessions[i], username, roles)
		if access == "" {
			continue
		}
		item := cloneRemoteSession(s.remoteSessions[i])
		item.AccessMode = access
		item.CurrentUser = username
		item.CurrentRoles = append([]string(nil), roles...)
		s.populateRemoteSessionRuntime(&item)
		out = append(out, item)
	}
	return out
}

func (s *Service) RemoteSessionReplay(id, username string, roles []string) (RemoteSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	admin := remoteContainsString(roles, "admin") || remoteContainsString(roles, "platform-admin")
	for i := range s.remoteSessions {
		item := s.remoteSessions[i]
		if item.ID != id {
			continue
		}
		access := remoteSessionAccess(item, username, roles)
		if access == "" {
			continue
		}
		item = cloneRemoteSession(item)
		item.AccessMode = access
		item.CurrentUser = username
		item.CurrentRoles = append([]string(nil), roles...)
		s.populateRemoteSessionRuntime(&item)
		if admin && access == "" {
			item.AccessMode = "control"
		}
		return item, nil
	}
	return RemoteSession{}, ErrNotFound
}

func (s *Service) loadRemoteSessions() error {
	if s.db == nil {
		return nil
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	if _, err := s.db.Exec(`UPDATE remote_access_sessions SET status='interrupted',closed_at=CASE WHEN closed_at='' THEN $2 ELSE closed_at END,owner_node='',lease_heartbeat_at='',lease_expires_at='',updated_at=now() WHERE status='active' AND owner_node<>'' AND (owner_node=$1 OR NULLIF(lease_expires_at,'') IS NULL OR NULLIF(lease_expires_at,'')::timestamptz <= now())`, s.instanceID, now); err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE remote_access_sessions SET archive_status='failed', archive_error='archive task interrupted by service restart' WHERE archive_status='archiving'`); err != nil {
		return err
	}
	return s.syncRemoteSessionsFromDB()
}

func (s *Service) syncRemoteSessionsFromDB() error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.Query(`SELECT id,asset_id,asset_name,host,port,credential_id,operator_name,roles,collaborators,status,created_at,expires_at,recording_retention_days,closed_at,archive_status,archive_bucket,archive_key,archive_sha256,archive_size,archived_at,archive_error,archive_deleted_at,archive_delete_error,owner_node,lease_heartbeat_at,lease_expires_at,logs FROM remote_access_sessions ORDER BY updated_at DESC LIMIT 500`)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []RemoteSession{}
	for rows.Next() {
		var item RemoteSession
		var roles, collaborators, logs []byte
		if err = rows.Scan(&item.ID, &item.AssetID, &item.AssetName, &item.IP, &item.Port, &item.CredentialID, &item.Operator, &roles, &collaborators, &item.Status, &item.CreatedAt, &item.ExpiresAt, &item.RecordingRetentionDays, &item.ClosedAt, &item.ArchiveStatus, &item.ArchiveBucket, &item.ArchiveKey, &item.ArchiveSHA256, &item.ArchiveSize, &item.ArchivedAt, &item.ArchiveError, &item.ArchiveDeletedAt, &item.ArchiveDeleteError, &item.OwnerNode, &item.LeaseHeartbeatAt, &item.LeaseExpiresAt, &logs); err != nil {
			return err
		}
		_ = json.Unmarshal(roles, &item.Roles)
		_ = json.Unmarshal(collaborators, &item.Collaborators)
		_ = json.Unmarshal(logs, &item.Logs)
		if item.Roles == nil {
			item.Roles = []string{}
		}
		if item.Collaborators == nil {
			item.Collaborators = []RemoteSessionCollaborator{}
		}
		if item.Logs == nil {
			item.Logs = []RemoteSessionLog{}
		}
		out = append(out, item)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.remoteSessions = out
	s.mu.Unlock()
	return nil
}

func (s *Service) RunRemoteSessionSync(ctx context.Context) {
	if s.db == nil {
		return
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.syncRemoteSessionsFromDB(); err != nil {
				slog.Error("sync remote sessions", "error", err)
			}
		}
	}
}

func (s *Service) persistRemoteSession(item RemoteSession) error {
	if s.db == nil {
		return nil
	}
	if item.Roles == nil {
		item.Roles = []string{}
	}
	if item.Collaborators == nil {
		item.Collaborators = []RemoteSessionCollaborator{}
	}
	if item.Logs == nil {
		item.Logs = []RemoteSessionLog{}
	}
	roles, _ := json.Marshal(item.Roles)
	collaborators, _ := json.Marshal(item.Collaborators)
	logs, _ := json.Marshal(item.Logs)
	_, err := s.db.Exec(`INSERT INTO remote_access_sessions(id,asset_id,asset_name,host,port,credential_id,operator_name,roles,collaborators,status,created_at,expires_at,recording_retention_days,closed_at,archive_status,archive_bucket,archive_key,archive_sha256,archive_size,archived_at,archive_error,archive_deleted_at,archive_delete_error,owner_node,lease_heartbeat_at,lease_expires_at,logs,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,now()) ON CONFLICT(id) DO UPDATE SET asset_id=excluded.asset_id,asset_name=excluded.asset_name,host=excluded.host,port=excluded.port,credential_id=excluded.credential_id,operator_name=excluded.operator_name,roles=excluded.roles,collaborators=excluded.collaborators,status=excluded.status,created_at=excluded.created_at,expires_at=excluded.expires_at,recording_retention_days=excluded.recording_retention_days,closed_at=excluded.closed_at,archive_status=excluded.archive_status,archive_bucket=excluded.archive_bucket,archive_key=excluded.archive_key,archive_sha256=excluded.archive_sha256,archive_size=excluded.archive_size,archived_at=excluded.archived_at,archive_error=excluded.archive_error,archive_deleted_at=excluded.archive_deleted_at,archive_delete_error=excluded.archive_delete_error,owner_node=excluded.owner_node,lease_heartbeat_at=excluded.lease_heartbeat_at,lease_expires_at=excluded.lease_expires_at,logs=excluded.logs,updated_at=now()`, item.ID, item.AssetID, item.AssetName, item.IP, item.Port, item.CredentialID, item.Operator, roles, collaborators, item.Status, item.CreatedAt, item.ExpiresAt, item.RecordingRetentionDays, item.ClosedAt, item.ArchiveStatus, item.ArchiveBucket, item.ArchiveKey, item.ArchiveSHA256, item.ArchiveSize, item.ArchivedAt, item.ArchiveError, item.ArchiveDeletedAt, item.ArchiveDeleteError, item.OwnerNode, item.LeaseHeartbeatAt, item.LeaseExpiresAt, logs)
	return err
}
