package toolbox

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

func (s *Service) runKubernetesPod(ctx context.Context, in RunInput) ([]Line, string, string, error) {
	if s.kubernetes == nil {
		return nil, "failed", "Kubernetes 诊断未配置", ErrNotConfigured
	}
	diagnosis, err := s.kubernetes.DiagnosePod(ctx, strings.TrimSpace(in.Namespace), strings.TrimSpace(in.Name), strings.TrimSpace(in.Container))
	if err != nil {
		return []Line{outputLine("error", err.Error())}, "failed", "Pod 诊断失败", nil
	}
	status := "success"
	if !strings.EqualFold(diagnosis.Phase, "Running") && !strings.EqualFold(diagnosis.Phase, "Succeeded") {
		status = "warning"
	}
	if diagnosis.RestartCount > 0 {
		status = "warning"
	}
	lines := []Line{
		outputLine("info", fmt.Sprintf("Pod：%s/%s", diagnosis.Namespace, diagnosis.Name)),
		outputLine("info", fmt.Sprintf("阶段：%s，节点：%s，Pod IP：%s", diagnosis.Phase, diagnosis.NodeName, diagnosis.PodIP)),
		outputLine("info", fmt.Sprintf("容器：%d，重启次数：%d", len(diagnosis.Containers), diagnosis.RestartCount)),
	}
	for _, item := range diagnosis.Containers {
		level := "success"
		if !item.Ready {
			level = "warning"
		}
		lines = append(lines, outputLine(level, fmt.Sprintf("容器 %s：Ready=%t，Restarts=%d，镜像=%s", item.Name, item.Ready, item.RestartCount, item.Image)))
	}
	for _, event := range diagnosis.Events {
		level := "info"
		if strings.EqualFold(event.Type, "Warning") {
			level = "warning"
		}
		lines = append(lines, outputLine(level, fmt.Sprintf("事件 [%s] %s：%s", event.Type, event.Reason, event.Message)))
	}
	if len(diagnosis.Logs) > 0 {
		lines = append(lines, outputLine("info", "最近日志："))
		for _, item := range diagnosis.Logs {
			lines = append(lines, outputLine("info", item))
		}
	}
	return lines, status, fmt.Sprintf("Pod 状态 %s，重启 %d 次", diagnosis.Phase, diagnosis.RestartCount), nil
}

func (s *Service) resolveSSHClient(ctx context.Context, in RunInput) (*ssh.Client, string, error) {
	if s.credentials == nil {
		return nil, "", ErrNotConfigured
	}
	host, port, address, err := normalizeHostPort(in.Target, "22")
	if err != nil {
		return nil, "", err
	}
	material, err := s.credentials(strings.TrimSpace(in.CredentialID))
	if err != nil {
		return nil, "", fmt.Errorf("解析 SSH 凭据失败：%w", err)
	}
	if strings.TrimSpace(material.Username) == "" || material.Secret == "" {
		return nil, "", fmt.Errorf("SSH 凭据不完整")
	}
	config := &ssh.ClientConfig{User: material.Username, Auth: []ssh.AuthMethod{ssh.Password(material.Secret)}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 8 * time.Second}
	conn, err := safeDialContext(ctx, "tcp", address)
	if err != nil {
		return nil, "", err
	}
	clientConn, channels, requests, err := ssh.NewClientConn(conn, net.JoinHostPort(host, port), config)
	if err != nil {
		_ = conn.Close()
		return nil, "", err
	}
	return ssh.NewClient(clientConn, channels, requests), address, nil
}

func (s *Service) runSSHScript(ctx context.Context, in RunInput, script string) (string, error) {
	client, _, err := s.resolveSSHClient(ctx, in)
	if err != nil {
		return "", err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	type commandResult struct {
		output []byte
		err    error
	}
	done := make(chan commandResult, 1)
	go func() {
		output, commandErr := session.CombinedOutput(script)
		done <- commandResult{output: output, err: commandErr}
	}()
	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGTERM)
		return "", ctx.Err()
	case result := <-done:
		if result.err != nil {
			return string(result.output), result.err
		}
		return string(result.output), nil
	}
}
func (s *Service) runHostHealth(ctx context.Context, in RunInput) ([]Line, string, string, error) {
	script := `printf 'hostname: '; hostname; printf 'uptime: '; uptime; printf 'memory: '; free -m | awk '/Mem:/{print $3"/"$2" MB"}'; printf 'disk: '; df -h / | awk 'NR==2{print $3"/"$2" ("$5")"}'; printf 'load: '; cat /proc/loadavg`
	output, err := s.runSSHScript(ctx, in, script)
	if err != nil {
		return []Line{outputLine("error", err.Error())}, "failed", "SSH 主机检查失败", nil
	}
	return textOutputLines(output, 80), "success", "主机健康检查完成", nil
}

func (s *Service) runHostProcess(ctx context.Context, in RunInput) ([]Line, string, string, error) {
	script := `ps -eo pid=,user=,comm=,args= | grep -F -- ` + shellQuote(strings.TrimSpace(in.Query)) + ` | grep -v grep || true`
	output, err := s.runSSHScript(ctx, in, script)
	if err != nil {
		return []Line{outputLine("error", err.Error())}, "failed", "进程查询失败", nil
	}
	lines := textOutputLines(output, 100)
	if strings.TrimSpace(output) == "" {
		return lines, "warning", "未找到匹配进程", nil
	}
	return lines, "success", fmt.Sprintf("查询到 %d 行进程信息", len(lines)), nil
}

func (s *Service) runHostPort(ctx context.Context, in RunInput) ([]Line, string, string, error) {
	port, err := strconv.Atoi(strings.TrimSpace(in.Query))
	if err != nil || port < 1 || port > 65535 {
		return nil, "failed", "端口校验失败", fmt.Errorf("端口必须是 1-65535")
	}
	script := `(ss -lntup 2>/dev/null || netstat -lntup 2>/dev/null) | grep -E '[:.]` + strconv.Itoa(port) + `[[:space:]]' || true`
	output, err := s.runSSHScript(ctx, in, script)
	if err != nil {
		return []Line{outputLine("error", err.Error())}, "failed", "端口查询失败", nil
	}
	lines := textOutputLines(output, 100)
	if strings.TrimSpace(output) == "" {
		return lines, "warning", fmt.Sprintf("端口 %d 未监听", port), nil
	}
	return lines, "success", fmt.Sprintf("端口 %d 正在监听", port), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func textOutputLines(value string, limit int) []Line {
	value = strings.TrimSpace(value)
	if value == "" {
		return []Line{outputLine("info", "命令未返回输出")}
	}
	raw := strings.Split(value, "\n")
	total := len(raw)
	if len(raw) > limit {
		raw = raw[:limit]
	}
	lines := make([]Line, 0, len(raw)+1)
	for _, item := range raw {
		lines = append(lines, outputLine("info", strings.TrimRight(item, "\r")))
	}
	if total > limit {
		lines = append(lines, outputLine("warning", "输出已截断"))
	}
	return lines
}
