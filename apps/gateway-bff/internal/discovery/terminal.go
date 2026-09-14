package discovery

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type TerminalTicket struct {
	Ticket    string `json:"ticket"`
	ExpiresAt string `json:"expiresAt"`
}

type terminalTicketRecord struct {
	SessionID string
	Operator  string
	Roles     []string
	ExpiresAt time.Time
}

type RemoteTerminalEvent struct {
	Sequence  int64  `json:"sequence"`
	Direction string `json:"direction"`
	Data      string `json:"data"`
	CreatedAt string `json:"createdAt"`
}

type TerminalRecording struct {
	Events    []RemoteTerminalEvent `json:"events"`
	Truncated bool                  `json:"truncated"`
}

type RemoteTerminalChannel struct {
	client    *ssh.Client
	session   *ssh.Session
	stdin     io.WriteCloser
	stdout    io.Reader
	service   *Service
	sessionID string
	closeOnce sync.Once
	closeErr  error
}

var blockedTerminalPatterns = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	{regexp.MustCompile(`(?i)(^|[;&|]\s*)(sudo\s+)?rm\s+(-[a-z]*r[a-z]*f?|-[a-z]*f[a-z]*r)\s+(/\s*$|/\*\s*$|/(etc|usr|var|boot|bin|sbin|lib|lib64|root)\b)`), "禁止递归强制删除系统目录"},
	{regexp.MustCompile(`(?i)\bmkfs(\.[a-z0-9]+)?\b`), "禁止格式化文件系统"},
	{regexp.MustCompile(`(?i)\b(fdisk|parted|sgdisk)\b`), "禁止修改磁盘分区"},
	{regexp.MustCompile(`(?i)\bdd\b[^\n]*\bof=/dev/(sd|nvme|vd|hd|mapper)`), "禁止直接覆写块设备"},
	{regexp.MustCompile(`(?i)(^|[;&|]\s*)(sudo\s+)?(shutdown|reboot|poweroff|halt|init\s+0)\b`), "禁止关闭或重启主机"},
	{regexp.MustCompile(`(?i)(^|[;&|]\s*)(sudo\s+)?kill\s+-9\s+1\b`), "禁止终止 init/systemd 主进程"},
	{regexp.MustCompile(`(?i)(^|[;&|]\s*)(sudo\s+)?chmod\s+-R\s+777\s+/\s*$`), "禁止递归开放根目录权限"},
	{regexp.MustCompile(`(?i):\(\)\s*\{\s*:\|:&\s*\};:`), "禁止执行 Fork Bomb"},
}

func (s *Service) CreateTerminalTicket(sessionID, operator string, roles []string) (TerminalTicket, error) {
	item, _, err := s.sessionForOperator(sessionID, operator, roles)
	if err != nil {
		return TerminalTicket{}, err
	}
	if !s.authorizeRemoteAccess(operator, roles, item.AssetID, "", "terminal") {
		return TerminalTicket{}, ErrRemoteValidation
	}
	data := make([]byte, 32)
	if _, err = rand.Read(data); err != nil {
		return TerminalTicket{}, err
	}
	expires := time.Now().Add(60 * time.Second)
	ticket := hex.EncodeToString(data)
	s.mu.Lock()
	for key, record := range s.terminalTickets {
		if record.ExpiresAt.Before(time.Now()) {
			delete(s.terminalTickets, key)
		}
	}
	s.terminalTickets[ticket] = terminalTicketRecord{SessionID: item.ID, Operator: operator, Roles: append([]string(nil), roles...), ExpiresAt: expires}
	s.mu.Unlock()
	return TerminalTicket{Ticket: ticket, ExpiresAt: expires.Format("2006-01-02 15:04:05")}, nil
}

func (s *Service) ConsumeTerminalTicket(ticket string) (RemoteSession, error) {
	ticket = strings.TrimSpace(ticket)
	if ticket == "" {
		return RemoteSession{}, ErrRemoteValidation
	}
	s.mu.Lock()
	record, ok := s.terminalTickets[ticket]
	if ok {
		delete(s.terminalTickets, ticket)
	}
	s.mu.Unlock()
	if !ok || record.ExpiresAt.Before(time.Now()) {
		return RemoteSession{}, ErrNotFound
	}
	item, _, err := s.sessionForOperator(record.SessionID, record.Operator, record.Roles)
	if err != nil {
		return RemoteSession{}, err
	}
	return item, nil
}

func (s *Service) OpenRemoteTerminal(sessionID, operator string, roles []string, cols, rows int) (*RemoteTerminalChannel, error) {
	item, _, err := s.sessionForOperator(sessionID, operator, roles)
	if err != nil {
		return nil, err
	}
	if !s.authorizeRemoteAccess(operator, roles, item.AssetID, "", "terminal") {
		return nil, ErrRemoteValidation
	}
	if cols < 40 {
		cols = 120
	}
	if rows < 12 {
		rows = 32
	}
	if cols > 400 {
		cols = 400
	}
	if rows > 200 {
		rows = 200
	}
	username, secret, err := s.resolveRemoteCredential(item.CredentialID)
	if err != nil {
		return nil, err
	}
	client, err := s.dialTrusted(item.AssetID, item.IP, item.Port, username, secret)
	if err != nil {
		return nil, err
	}
	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	if err = session.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}
	if err = session.Shell(); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}
	channel := &RemoteTerminalChannel{client: client, session: session, stdin: stdin, stdout: stdout, service: s, sessionID: sessionID}
	s.mu.Lock()
	s.terminals[sessionID] = channel
	s.mu.Unlock()
	return channel, nil
}

func (c *RemoteTerminalChannel) Read(p []byte) (int, error) {
	return c.stdout.Read(p)
}

func (c *RemoteTerminalChannel) Write(p []byte) (int, error) {
	return c.stdin.Write(p)
}

func (c *RemoteTerminalChannel) Resize(cols, rows int) error {
	if cols < 40 || rows < 12 || cols > 400 || rows > 200 {
		return ErrRemoteValidation
	}
	return c.session.WindowChange(rows, cols)
}

func (c *RemoteTerminalChannel) Close() error {
	c.closeOnce.Do(func() {
		_ = c.stdin.Close()
		_ = c.session.Close()
		c.closeErr = c.client.Close()
		if c.service != nil {
			c.service.mu.Lock()
			if c.service.terminals[c.sessionID] == c {
				delete(c.service.terminals, c.sessionID)
			}
			c.service.mu.Unlock()
		}
	})
	return c.closeErr
}

func (s *Service) CloseRemoteTerminal(sessionID string) {
	s.mu.RLock()
	channel := s.terminals[sessionID]
	s.mu.RUnlock()
	if channel != nil {
		_ = channel.Close()
	}
}

func (s *Service) AppendTerminalEvent(sessionID, direction, data string) error {
	if strings.TrimSpace(sessionID) == "" || data == "" || len(data) > 256<<10 {
		return nil
	}
	s.mu.Lock()
	if s.terminalSequences == nil {
		s.terminalSequences = map[string]uint64{}
	}
	s.terminalSequences[sessionID]++
	sequence := s.terminalSequences[sessionID]
	s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(data))
	_, err := s.db.Exec(`INSERT INTO remote_terminal_events(session_id,sequence,direction,data) VALUES($1,$2,$3,$4)`, sessionID, sequence, direction, encoded)
	return err
}

func (s *Service) TerminalRecording(sessionID, username string, roles []string) (TerminalRecording, error) {
	if _, err := s.RemoteSessionReplay(sessionID, username, roles); err != nil {
		return TerminalRecording{}, err
	}
	result := TerminalRecording{Events: []RemoteTerminalEvent{}}
	if s.db == nil {
		return result, nil
	}
	rows, err := s.db.Query(`SELECT sequence,direction,data,created_at FROM remote_terminal_events WHERE session_id=$1 ORDER BY sequence LIMIT 20001`, sessionID)
	if err != nil {
		return TerminalRecording{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item RemoteTerminalEvent
		var encoded string
		var created time.Time
		if err = rows.Scan(&item.Sequence, &item.Direction, &encoded, &created); err != nil {
			return TerminalRecording{}, err
		}
		item.Data = encoded
		item.CreatedAt = created.Local().Format("2006-01-02 15:04:05.000")
		if len(result.Events) >= 20000 {
			result.Truncated = true
			break
		}
		result.Events = append(result.Events, item)
	}
	return result, rows.Err()
}

func (s *Service) RecordRemoteSessionEvent(sessionID, level, kind, message string) {
	_, _ = s.appendSessionLog(sessionID, level, kind, message, 0)
}

func BlockedTerminalCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	for _, item := range blockedTerminalPatterns {
		if item.pattern.MatchString(command) {
			return item.reason
		}
	}
	return ""
}
