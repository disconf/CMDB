package discovery

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"path"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type AccessGrant struct {
	ID           string   `json:"id"`
	SubjectType  string   `json:"subjectType"`
	Subject      string   `json:"subject"`
	AssetID      string   `json:"assetId"`
	ProjectGroup string   `json:"projectGroup"`
	Permissions  []string `json:"permissions"`
	Enabled      bool     `json:"enabled"`
	CreatedBy    string   `json:"createdBy"`
}

type AccessGrantInput struct {
	SubjectType  string   `json:"subjectType"`
	Subject      string   `json:"subject"`
	AssetID      string   `json:"assetId"`
	ProjectGroup string   `json:"projectGroup"`
	Permissions  []string `json:"permissions"`
	Enabled      *bool    `json:"enabled"`
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
	AssetID     string `json:"assetId"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	KeyType     string `json:"keyType"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"publicKey"`
	Trusted     bool   `json:"trusted"`
}
type RemoteSessionLog struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type RemoteSession struct {
	ID           string             `json:"id"`
	AssetID      string             `json:"assetId"`
	AssetName    string             `json:"assetName"`
	IP           string             `json:"ip"`
	Port         int                `json:"port"`
	CredentialID string             `json:"credentialId"`
	Operator     string             `json:"operator"`
	Roles        []string           `json:"roles"`
	Status       string             `json:"status"`
	CreatedAt    string             `json:"createdAt"`
	ExpiresAt    string             `json:"expiresAt"`
	ClosedAt     string             `json:"closedAt,omitempty"`
	Logs         []RemoteSessionLog `json:"logs"`
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
		_ = s.persistAccessGrant(item)
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
	if (input.SubjectType != "user" && input.SubjectType != "role") || input.Subject == "" || (strings.TrimSpace(input.AssetID) == "" && strings.TrimSpace(input.ProjectGroup) == "") {
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
	return AccessGrant{SubjectType: input.SubjectType, Subject: input.Subject, AssetID: strings.TrimSpace(input.AssetID), ProjectGroup: strings.TrimSpace(input.ProjectGroup), Permissions: permissions, Enabled: enabled}, nil
}
func (s *Service) authorizeRemoteAccess(username string, roles []string, assetID, projectGroup, permission string) bool {
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
		if !match {
			continue
		}
		if grant.AssetID != "" && grant.AssetID != assetID {
			continue
		}
		if grant.ProjectGroup != "" && grant.ProjectGroup != projectGroup {
			continue
		}
		if remoteContainsString(grant.Permissions, permission) {
			return true
		}
	}
	return false
}
func cloneAccessGrant(item AccessGrant) AccessGrant {
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
	admin := remoteContainsString(roles, "admin") || remoteContainsString(roles, "platform-admin")
	out := []RemoteSession{}
	for i := range s.remoteSessions {
		if s.remoteSessions[i].Status == "closed" || parseRemoteTime(s.remoteSessions[i].ExpiresAt).Before(now) {
			continue
		}
		if !admin && s.remoteSessions[i].Operator != username {
			continue
		}
		out = append(out, cloneRemoteSession(s.remoteSessions[i]))
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
	if !s.authorizeRemoteAccess(operator, roles, asset.ID, asset.ProjectGroup, "terminal") {
		return RemoteSession{}, ErrRemoteValidation
	}
	credentialID = strings.TrimSpace(credentialID)
	if _, _, err = s.resolveRemoteCredential(credentialID); err != nil {
		return RemoteSession{}, err
	}
	now := time.Now()
	item := RemoteSession{ID: remoteSessionID(), AssetID: asset.ID, AssetName: asset.Name, IP: asset.IP, Port: 22, CredentialID: credentialID, Operator: operator, Roles: append([]string(nil), roles...), Status: "active", CreatedAt: now.Format("2006-01-02 15:04:05"), ExpiresAt: now.Add(30 * time.Minute).Format("2006-01-02 15:04:05"), Logs: []RemoteSessionLog{{Time: now.Format("15:04:05"), Level: "success", Message: "远程会话已建立"}}}
	s.mu.Lock()
	s.remoteSessions = append([]RemoteSession{item}, s.remoteSessions...)
	s.mu.Unlock()
	return cloneRemoteSession(item), nil
}
func (s *Service) ExecuteRemoteSession(sessionID, command, operator string, roles []string) (RemoteSession, error) {
	command = strings.TrimSpace(command)
	if command == "" || len(command) > 4000 {
		return RemoteSession{}, ErrRemoteValidation
	}
	item, index, err := s.sessionForOperator(sessionID, operator, roles)
	if err != nil {
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
		message, level = runErr.Error(), "error"
	}
	s.mu.Lock()
	if index < 0 || index >= len(s.remoteSessions) {
		s.mu.Unlock()
		return RemoteSession{}, ErrNotFound
	}
	s.remoteSessions[index].Logs = append(s.remoteSessions[index].Logs, RemoteSessionLog{Time: started.Format("15:04:05"), Level: level, Message: "$ " + command + "\n" + message + "\n(" + time.Since(started).Round(time.Millisecond).String() + ")"})
	result := cloneRemoteSession(s.remoteSessions[index])
	s.mu.Unlock()
	return result, runErr
}
func (s *Service) UploadRemoteFile(sessionID, remotePath string, reader io.Reader, size int64, operator string, roles []string) error {
	if size <= 0 || size > 50<<20 {
		return ErrRemoteValidation
	}
	item, _, err := s.sessionForOperator(sessionID, operator, roles)
	if err != nil {
		return err
	}
	if !s.authorizeRemoteAccess(operator, roles, item.AssetID, "", "file") {
		return ErrRemoteValidation
	}
	if err = validateRemotePath(remotePath); err != nil {
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
	s.appendSessionLog(sessionID, "success", "上传文件: "+remotePath)
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
	if !s.authorizeRemoteAccess(operator, roles, item.AssetID, "", "file") {
		return nil, ErrRemoteValidation
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
	if len(output) > 25<<20 {
		return nil, fmt.Errorf("remote file exceeds 25MB")
	}
	s.appendSessionLog(sessionID, "success", "下载文件: "+remotePath)
	return output, nil
}
func (s *Service) CloseRemoteSession(sessionID, operator string, roles []string) error {
	_, index, err := s.sessionForOperator(sessionID, operator, roles)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.remoteSessions[index].Status = "closed"
	s.remoteSessions[index].ClosedAt = time.Now().Format("2006-01-02 15:04:05")
	s.mu.Unlock()
	return nil
}
func (s *Service) sessionForOperator(id, operator string, roles []string) (RemoteSession, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i, item := range s.remoteSessions {
		if item.ID == id {
			if item.Status != "active" || parseRemoteTime(item.ExpiresAt).Before(time.Now()) {
				return RemoteSession{}, -1, ErrRemoteValidation
			}
			if item.Operator != operator && !remoteContainsString(roles, "admin") && !remoteContainsString(roles, "platform-admin") {
				return RemoteSession{}, -1, ErrRemoteValidation
			}
			return cloneRemoteSession(item), i, nil
		}
	}
	return RemoteSession{}, -1, ErrNotFound
}
func (s *Service) appendSessionLog(id, level, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.remoteSessions {
		if s.remoteSessions[i].ID == id {
			s.remoteSessions[i].Logs = append(s.remoteSessions[i].Logs, RemoteSessionLog{Time: time.Now().Format("15:04:05"), Level: level, Message: message})
			return
		}
	}
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
	item.Logs = append([]RemoteSessionLog(nil), item.Logs...)
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
	rows, err := s.db.Query(`SELECT id,subject_type,subject,asset_id,project_group,permissions,enabled,created_by FROM remote_access_grants ORDER BY updated_at DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []AccessGrant{}
	for rows.Next() {
		var item AccessGrant
		var raw []byte
		if err = rows.Scan(&item.ID, &item.SubjectType, &item.Subject, &item.AssetID, &item.ProjectGroup, &raw, &item.Enabled, &item.CreatedBy); err != nil {
			return err
		}
		if json.Unmarshal(raw, &item.Permissions) != nil {
			item.Permissions = []string{}
		}
		out = append(out, item)
	}
	s.accessGrants = out
	return rows.Err()
}
func (s *Service) persistAccessGrant(item AccessGrant) error {
	if s.db == nil {
		return nil
	}
	raw, _ := json.Marshal(item.Permissions)
	_, err := s.db.Exec(`INSERT INTO remote_access_grants(id,subject_type,subject,asset_id,project_group,permissions,enabled,created_by,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,now()) ON CONFLICT(id) DO UPDATE SET subject_type=excluded.subject_type,subject=excluded.subject,asset_id=excluded.asset_id,project_group=excluded.project_group,permissions=excluded.permissions,enabled=excluded.enabled,updated_at=now()`, item.ID, item.SubjectType, item.Subject, item.AssetID, item.ProjectGroup, raw, item.Enabled, item.CreatedBy)
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
	if !s.authorizeRemoteAccess(operator, roles, asset.ID, asset.ProjectGroup, "terminal") {
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
	probe.Trusted = s.findTrustedHostKeyLocked(asset.ID, asset.IP, 22, fingerprint)
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
