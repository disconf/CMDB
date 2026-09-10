package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

var ErrRemoteValidation = errors.New("remote execution validation failed")

type RemoteOperation struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Command     string `json:"command"`
	Risk        string `json:"risk"`
}

type RemoteExecutionInput struct {
	OperationID  string   `json:"operationId"`
	Targets      []string `json:"targets"`
	Port         int      `json:"port"`
	CredentialID string   `json:"credentialId"`
	RequestedBy  string   `json:"requestedBy"`
}

type RemoteTargetResult struct {
	Target   string `json:"target"`
	Status   string `json:"status"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
	Duration string `json:"duration,omitempty"`
}

type RemoteExecution struct {
	ID            string               `json:"id"`
	OperationID   string               `json:"operationId"`
	OperationName string               `json:"operationName"`
	Targets       []string             `json:"targets"`
	Port          int                  `json:"port"`
	CredentialID  string               `json:"credentialId,omitempty"`
	Status        string               `json:"status"`
	RequestedBy   string               `json:"requestedBy"`
	ApprovedBy    string               `json:"approvedBy,omitempty"`
	CreatedAt     string               `json:"createdAt"`
	ApprovedAt    string               `json:"approvedAt,omitempty"`
	StartedAt     string               `json:"startedAt,omitempty"`
	FinishedAt    string               `json:"finishedAt,omitempty"`
	Results       []RemoteTargetResult `json:"results"`
}

type RemoteRunner func(ctx context.Context, target string, port int, command, username, secret string, timeout time.Duration) (string, error)

var remoteOperations = []RemoteOperation{
	{ID: "uptime", Name: "运行时长与负载", Description: "查看系统启动时间和 1/5/15 分钟负载", Command: "uptime", Risk: "low"},
	{ID: "system-info", Name: "系统版本信息", Description: "查看内核、架构和主机名", Command: "uname -a", Risk: "low"},
	{ID: "disk-usage", Name: "磁盘使用率", Description: "按文件系统查看磁盘容量和使用率", Command: "df -hT -x tmpfs -x devtmpfs", Risk: "low"},
	{ID: "memory-usage", Name: "内存使用率", Description: "查看内存和交换分区使用情况", Command: "free -m", Risk: "low"},
	{ID: "agent-status", Name: "CMDB Agent 状态", Description: "检查 cmdb-agent systemd 服务状态", Command: "systemctl is-active cmdb-agent 2>/dev/null || true", Risk: "low"},
	{ID: "listen-ports", Name: "监听端口", Description: "查看 TCP 监听端口，不修改系统状态", Command: "ss -lnt 2>/dev/null || netstat -lnt 2>/dev/null || true", Risk: "low"},
}

var targetPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{0,252}[a-zA-Z0-9]$`)

func (s *Service) RemoteOperations() []RemoteOperation {
	return append([]RemoteOperation(nil), remoteOperations...)
}

func remoteOperation(id string) (RemoteOperation, bool) {
	for _, operation := range remoteOperations {
		if operation.ID == id {
			return operation, true
		}
	}
	return RemoteOperation{}, false
}

func normalizeTargets(values []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		target := strings.TrimSpace(value)
		if target == "" || seen[target] {
			continue
		}
		if net.ParseIP(target) == nil && !targetPattern.MatchString(target) {
			return nil, ErrRemoteValidation
		}
		seen[target] = true
		out = append(out, target)
		if len(out) > 20 {
			return nil, ErrRemoteValidation
		}
	}
	if len(out) == 0 {
		return nil, ErrRemoteValidation
	}
	return out, nil
}

func (s *Service) CreateRemoteExecution(in RemoteExecutionInput) (RemoteExecution, error) {
	operation, ok := remoteOperation(strings.TrimSpace(in.OperationID))
	if !ok || strings.TrimSpace(in.RequestedBy) == "" {
		return RemoteExecution{}, ErrRemoteValidation
	}
	targets, err := normalizeTargets(in.Targets)
	if err != nil {
		return RemoteExecution{}, err
	}
	if in.Port == 0 {
		in.Port = 22
	}
	if in.Port < 1 || in.Port > 65535 {
		return RemoteExecution{}, ErrRemoteValidation
	}
	now := time.Now()
	execution := RemoteExecution{
		ID: fmt.Sprintf("rexec-%d", now.UnixNano()), OperationID: operation.ID, OperationName: operation.Name,
		Targets: targets, Port: in.Port, CredentialID: strings.TrimSpace(in.CredentialID), Status: "awaiting_approval",
		RequestedBy: in.RequestedBy, CreatedAt: now.Format("2006-01-02 15:04:05"), Results: make([]RemoteTargetResult, 0, len(targets)),
	}
	for _, target := range targets {
		execution.Results = append(execution.Results, RemoteTargetResult{Target: target, Status: "pending"})
	}
	s.mu.Lock()
	s.remoteExecutions = append([]RemoteExecution{execution}, s.remoteExecutions...)
	s.mu.Unlock()
	if err := s.persistRemoteExecution(execution); err != nil {
		s.mu.Lock()
		for i := range s.remoteExecutions {
			if s.remoteExecutions[i].ID == execution.ID {
				s.remoteExecutions = append(s.remoteExecutions[:i], s.remoteExecutions[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
		return RemoteExecution{}, err
	}
	return cloneRemoteExecution(execution), nil
}

func (s *Service) RemoteExecutions() []RemoteExecution {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RemoteExecution, len(s.remoteExecutions))
	for i := range s.remoteExecutions {
		out[i] = cloneRemoteExecution(s.remoteExecutions[i])
	}
	return out
}

func (s *Service) RemoteExecution(id string) (RemoteExecution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, execution := range s.remoteExecutions {
		if execution.ID == id {
			return cloneRemoteExecution(execution), nil
		}
	}
	return RemoteExecution{}, ErrNotFound
}

func (s *Service) ApproveRemoteExecution(id, approver string) (RemoteExecution, error) {
	if strings.TrimSpace(approver) == "" {
		return RemoteExecution{}, ErrRemoteValidation
	}
	s.mu.Lock()
	index := -1
	for i := range s.remoteExecutions {
		if s.remoteExecutions[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		s.mu.Unlock()
		return RemoteExecution{}, ErrNotFound
	}
	if s.remoteExecutions[index].Status != "awaiting_approval" {
		s.mu.Unlock()
		return RemoteExecution{}, ErrRemoteValidation
	}
	now := time.Now()
	s.remoteExecutions[index].Status = "running"
	s.remoteExecutions[index].ApprovedBy = approver
	s.remoteExecutions[index].ApprovedAt = now.Format("2006-01-02 15:04:05")
	s.remoteExecutions[index].StartedAt = now.Format("2006-01-02 15:04:05")
	execution := cloneRemoteExecution(s.remoteExecutions[index])
	s.mu.Unlock()
	if err := s.persistRemoteExecution(execution); err != nil {
		return RemoteExecution{}, err
	}
	go s.runRemoteExecution(id)
	return execution, nil
}

func (s *Service) runRemoteExecution(id string) {
	execution, err := s.RemoteExecution(id)
	if err != nil {
		return
	}
	operation, ok := remoteOperation(execution.OperationID)
	if !ok {
		s.finishRemoteExecution(id, "failed", "operation is no longer available")
		return
	}
	username, secret, err := s.resolveRemoteCredential(execution.CredentialID)
	if err != nil {
		s.finishRemoteExecution(id, "failed", err.Error())
		return
	}
	runner := s.remoteRunner
	if runner == nil {
		runner = sshRemoteRunner
	}
	for index, target := range execution.Targets {
		started := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		output, runErr := runner(ctx, target, execution.Port, operation.Command, username, secret, 15*time.Second)
		cancel()
		result := RemoteTargetResult{Target: target, Status: "success", Output: truncateOutput(output), Duration: time.Since(started).Round(time.Millisecond).String()}
		if runErr != nil {
			result.Status = "failed"
			result.Error = runErr.Error()
		}
		s.updateRemoteResult(id, index, result)
	}
	s.finishRemoteExecution(id, "", "")
}

func (s *Service) resolveRemoteCredential(credentialID string) (string, string, error) {
	if credentialID != "" {
		username, secret, err := s.resolveCredential(credentialID)
		if err != nil {
			return "", "", err
		}
		if username == "" || secret == "" {
			return "", "", errors.New("凭据内容为空")
		}
		return username, secret, nil
	}
	username, secret := os.Getenv("CMDB_SSH_USERNAME"), os.Getenv("CMDB_SSH_PASSWORD")
	if username == "" || secret == "" {
		return "", "", errors.New("未配置默认 SSH 凭据")
	}
	return username, secret, nil
}

func (s *Service) updateRemoteResult(id string, index int, result RemoteTargetResult) {
	s.mu.Lock()
	for i := range s.remoteExecutions {
		if s.remoteExecutions[i].ID != id || index < 0 || index >= len(s.remoteExecutions[i].Results) {
			continue
		}
		s.remoteExecutions[i].Results[index] = result
		execution := cloneRemoteExecution(s.remoteExecutions[i])
		s.mu.Unlock()
		_ = s.persistRemoteExecution(execution)
		return
	}
	s.mu.Unlock()
}

func (s *Service) finishRemoteExecution(id, status, message string) {
	s.mu.Lock()
	for i := range s.remoteExecutions {
		if s.remoteExecutions[i].ID != id {
			continue
		}
		if status == "" {
			success, failed := 0, 0
			for _, result := range s.remoteExecutions[i].Results {
				if result.Status == "success" {
					success++
				} else if result.Status == "failed" {
					failed++
				}
			}
			switch {
			case failed == 0:
				status = "success"
			case success == 0:
				status = "failed"
			default:
				status = "partial"
			}
		}
		s.remoteExecutions[i].Status = status
		s.remoteExecutions[i].FinishedAt = time.Now().Format("2006-01-02 15:04:05")
		if message != "" {
			for j := range s.remoteExecutions[i].Results {
				if s.remoteExecutions[i].Results[j].Status == "pending" || s.remoteExecutions[i].Results[j].Status == "running" {
					s.remoteExecutions[i].Results[j].Status = "failed"
					s.remoteExecutions[i].Results[j].Error = message
				}
			}
		}
		execution := cloneRemoteExecution(s.remoteExecutions[i])
		s.mu.Unlock()
		_ = s.persistRemoteExecution(execution)
		return
	}
	s.mu.Unlock()
}

func cloneRemoteExecution(execution RemoteExecution) RemoteExecution {
	execution.Targets = append([]string(nil), execution.Targets...)
	execution.Results = append([]RemoteTargetResult(nil), execution.Results...)
	return execution
}

func truncateOutput(output string) string {
	output = strings.TrimSpace(output)
	if len(output) > 16384 {
		return output[:16384] + "\n... output truncated"
	}
	return output
}

func sshRemoteRunner(ctx context.Context, target string, port int, command, username, secret string, timeout time.Duration) (string, error) {
	config := &ssh.ClientConfig{
		User:            username,
		Auth:            []ssh.AuthMethod{ssh.Password(secret)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}
	address := net.JoinHostPort(target, strconv.Itoa(port))
	client, err := ssh.Dial("tcp", address, config)
	if err != nil {
		return "", err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	done := make(chan struct{})
	var output []byte
	var runErr error
	go func() {
		output, runErr = session.CombinedOutput(command)
		close(done)
	}()
	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGTERM)
		_ = session.Close()
		<-done
		return string(output), ctx.Err()
	case <-done:
		return string(output), runErr
	}
}

func (s *Service) loadRemoteExecutions() error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.Query(`SELECT id,operation_id,operation_name,targets,port,credential_id,status,requested_by,approved_by,created_at,approved_at,started_at,finished_at,results FROM remote_executions ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []RemoteExecution{}
	for rows.Next() {
		var execution RemoteExecution
		var targets, results []byte
		if err := rows.Scan(&execution.ID, &execution.OperationID, &execution.OperationName, &targets, &execution.Port, &execution.CredentialID, &execution.Status, &execution.RequestedBy, &execution.ApprovedBy, &execution.CreatedAt, &execution.ApprovedAt, &execution.StartedAt, &execution.FinishedAt, &results); err != nil {
			return err
		}
		_ = json.Unmarshal(targets, &execution.Targets)
		_ = json.Unmarshal(results, &execution.Results)
		if execution.Status == "running" {
			execution.Status = "failed"
			for i := range execution.Results {
				if execution.Results[i].Status == "pending" || execution.Results[i].Status == "running" {
					execution.Results[i].Status = "failed"
					execution.Results[i].Error = "网关重启导致执行中断"
				}
			}
		}
		out = append(out, execution)
	}
	s.remoteExecutions = out
	return rows.Err()
}

func (s *Service) persistRemoteExecution(execution RemoteExecution) error {
	if s.db == nil {
		return nil
	}
	targets, _ := json.Marshal(execution.Targets)
	results, _ := json.Marshal(execution.Results)
	_, err := s.db.Exec(`INSERT INTO remote_executions(id,operation_id,operation_name,targets,port,credential_id,status,requested_by,approved_by,created_at,approved_at,started_at,finished_at,results,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,now()) ON CONFLICT(id) DO UPDATE SET port=excluded.port,status=excluded.status,approved_by=excluded.approved_by,approved_at=excluded.approved_at,started_at=excluded.started_at,finished_at=excluded.finished_at,results=excluded.results,updated_at=now()`, execution.ID, execution.OperationID, execution.OperationName, targets, execution.Port, execution.CredentialID, execution.Status, execution.RequestedBy, execution.ApprovedBy, execution.CreatedAt, execution.ApprovedAt, execution.StartedAt, execution.FinishedAt, results)
	return err
}
