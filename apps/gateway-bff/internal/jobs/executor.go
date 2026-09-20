package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type ExecutionRequest struct {
	JobID           string            `json:"jobId"`
	Command         string            `json:"command,omitempty"`
	Content         string            `json:"content,omitempty"`
	PlaybookID      string            `json:"playbookId,omitempty"`
	PlaybookVersion int               `json:"playbookVersion,omitempty"`
	Parameters      map[string]string `json:"parameters,omitempty"`
	CheckMode       bool              `json:"checkMode,omitempty"`
	Targets         []string          `json:"targets"`
	Operator        string            `json:"operator"`
	Attempt         int               `json:"attempt"`
	TimeoutSeconds  int               `json:"timeoutSeconds"`
	CredentialID    string            `json:"credentialId,omitempty"`
}
type ExecutionEvent struct {
	Progress int    `json:"progress"`
	Level    string `json:"level"`
	Message  string `json:"message"`
}
type ExecutionResult struct {
	Status       string           `json:"status"`
	Message      string           `json:"message"`
	Events       []ExecutionEvent `json:"events,omitempty"`
	Targets      []TargetResult   `json:"targets,omitempty"`
	ArtifactKeys []string         `json:"artifactKeys,omitempty"`
}
type ExecutorInfo struct {
	Mode     string `json:"mode"`
	Ready    bool   `json:"ready"`
	Endpoint string `json:"endpoint"`
}
type RunnerCapabilities struct {
	Mode            string   `json:"mode"`
	Ready           bool     `json:"ready"`
	Endpoint        string   `json:"endpoint"`
	Status          string   `json:"status"`
	Version         string   `json:"version"`
	Profiles        []string `json:"profiles"`
	MaxTargets      int      `json:"maxTargets"`
	ArtifactArchive bool     `json:"artifactArchive"`
	Features        []string `json:"features"`
	Error           string   `json:"error,omitempty"`
}
type Executor interface {
	Execute(context.Context, ExecutionRequest, func(ExecutionEvent)) ExecutionResult
	Info() ExecutorInfo
}
type simulatedExecutor struct{}

func (simulatedExecutor) Info() ExecutorInfo { return ExecutorInfo{Mode: "simulated", Ready: true} }
func (simulatedExecutor) Execute(ctx context.Context, input ExecutionRequest, emit func(ExecutionEvent)) ExecutionResult {
	steps := []string{"执行前安全检查完成", "目标连接检查完成", "作业指令已下发", "目标执行结果已回收", "执行后健康检查完成"}
	for i, msg := range steps {
		select {
		case <-ctx.Done():
			return ExecutionResult{Status: "cancelled", Message: "任务已取消或执行超时"}
		case <-time.After(350 * time.Millisecond):
			emit(ExecutionEvent{Progress: (i + 1) * 20, Level: "success", Message: msg})
		}
	}
	targets := make([]TargetResult, 0, len(input.Targets))
	for _, target := range input.Targets {
		targets = append(targets, TargetResult{Target: target, Status: "success", RC: 0, DurationMS: 1000})
	}
	return ExecutionResult{Status: "success", Message: "任务执行完成", Targets: targets}
}

type httpExecutor struct {
	endpoint, token string
	client          *http.Client
}

func (e *httpExecutor) Info() ExecutorInfo {
	return ExecutorInfo{Mode: "ansible-runner", Ready: e.endpoint != "", Endpoint: e.endpoint}
}
func (e *httpExecutor) Capabilities(ctx context.Context) (RunnerCapabilities, error) {
	result := RunnerCapabilities{Mode: "ansible-runner", Ready: e.endpoint != "", Endpoint: e.endpoint}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, e.endpoint+"/api/v1/capabilities", nil)
	if err != nil {
		return result, err
	}
	if e.token != "" {
		request.Header.Set("Authorization", "Bearer "+e.token)
	}
	response, err := e.client.Do(request)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, fmt.Errorf("runner capabilities returned %d", response.StatusCode)
	}
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		return result, err
	}
	result.Mode = "ansible-runner"
	result.Ready = true
	result.Endpoint = e.endpoint
	return result, nil
}
func (e *httpExecutor) DownloadArtifact(ctx context.Context, key string) (io.ReadCloser, error) {
	key = strings.TrimPrefix(strings.TrimSpace(key), "/")
	if !strings.HasPrefix(key, "jobs/") || !strings.HasSuffix(key, ".zip") {
		return nil, ErrNotFound
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, e.endpoint+"/api/v1/artifacts/"+key, nil)
	if err != nil {
		return nil, err
	}
	if e.token != "" {
		request.Header.Set("Authorization", "Bearer "+e.token)
	}
	response, err := e.client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_ = response.Body.Close()
		return nil, ErrNotFound
	}
	return response.Body, nil
}
func (e *httpExecutor) Execute(ctx context.Context, input ExecutionRequest, emit func(ExecutionEvent)) ExecutionResult {
	body, _ := json.Marshal(input)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint+"/api/v1/run", bytes.NewReader(body))
	if err != nil {
		return ExecutionResult{Status: "failed", Message: err.Error()}
	}
	request.Header.Set("Content-Type", "application/json")
	if e.token != "" {
		request.Header.Set("Authorization", "Bearer "+e.token)
	}
	response, err := e.client.Do(request)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return ExecutionResult{Status: "cancelled", Message: "Runner 请求已取消或超时"}
		}
		return ExecutionResult{Status: "failed", Message: "Runner 连接失败: " + err.Error()}
	}
	defer response.Body.Close()
	var result ExecutionResult
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		return ExecutionResult{Status: "failed", Message: "Runner 响应无效"}
	}
	for _, event := range result.Events {
		emit(event)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		result.Status = "failed"
	}
	if result.Status != "success" && result.Status != "failed" && result.Status != "cancelled" {
		result.Status = "failed"
	}
	return result
}
func newExecutorFromEnv() Executor {
	endpoint := strings.TrimRight(os.Getenv("ANSIBLE_RUNNER_URL"), "/")
	if endpoint == "" {
		return simulatedExecutor{}
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		panic(fmt.Sprintf("invalid ANSIBLE_RUNNER_URL: %q", endpoint))
	}
	return &httpExecutor{endpoint: endpoint, token: os.Getenv("ANSIBLE_RUNNER_TOKEN"), client: &http.Client{Transport: http.DefaultTransport}}
}
