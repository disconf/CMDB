package toolbox

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrValidation = errors.New("validation")

type Tool struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Placeholder string `json:"placeholder"`
	Risk        string `json:"risk"`
}
type Line struct {
	Time  string `json:"time"`
	Level string `json:"level"`
	Text  string `json:"text"`
}
type Result struct {
	ID        string `json:"id"`
	ToolID    string `json:"toolId"`
	ToolName  string `json:"toolName"`
	Target    string `json:"target"`
	Status    string `json:"status"`
	Operator  string `json:"operator"`
	StartedAt string `json:"startedAt"`
	Duration  string `json:"duration"`
	Summary   string `json:"summary"`
	Output    []Line `json:"output"`
}
type RunInput struct {
	ToolID   string `json:"toolId"`
	Target   string `json:"target"`
	Query    string `json:"query"`
	Operator string `json:"operator"`
}
type Summary struct {
	Tools           int     `json:"tools"`
	ExecutionsToday int     `json:"executionsToday"`
	SuccessRate     float64 `json:"successRate"`
	AvgDuration     string  `json:"avgDuration"`
}
type Service struct {
	mu      sync.RWMutex
	tools   []Tool
	history []Result
	next    int
}

func NewService() *Service {
	return &Service{tools: []Tool{{"ping", "连通性检测", "network", "检测目标网络延迟与丢包率", "Radio", "IP 或域名", "low"}, {"tcp", "端口探测", "network", "验证 TCP 服务端口可达性", "Network", "IP:端口", "low"}, {"host-health", "主机健康检查", "host", "检查负载、磁盘、内存和进程", "Server", "资产名称或 IP", "low"}, {"k8s-pod", "Pod 异常诊断", "kubernetes", "检查事件、重启次数与探针状态", "Boxes", "命名空间/Pod", "medium"}, {"log-search", "日志检索", "logs", "按关键字聚合检索应用日志", "Search", "应用名称", "low"}}, history: []Result{{"diag-001", "host-health", "主机健康检查", "prod-api-01", "success", "张伟", "14:35", "1.8s", "主机整体健康", []Line{{"14:35:01", "info", "CPU 负载 1.42"}, {"14:35:02", "success", "磁盘与内存状态正常"}}}, {"diag-002", "k8s-pod", "Pod 异常诊断", "prod/order-api-7f9c", "warning", "赵峰", "14:18", "2.4s", "发现 2 次容器重启", []Line{{"14:18:03", "warning", "Last State: OOMKilled"}}}}, next: 3}
}
func (s *Service) Tools() []Tool { return append([]Tool(nil), s.tools...) }
func (s *Service) History() []Result {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Result(nil), s.history...)
}
func (s *Service) Summary() Summary { return Summary{len(s.tools), len(s.history), 96.4, "2.1s"} }
func (s *Service) Run(in RunInput) (Result, error) {
	var tool *Tool
	for i := range s.tools {
		if s.tools[i].ID == in.ToolID {
			tool = &s.tools[i]
		}
	}
	if tool == nil || in.Target == "" {
		return Result{}, ErrValidation
	}
	now := time.Now()
	lines := []Line{{now.Format("15:04:05"), "info", fmt.Sprintf("启动 %s，目标 %s", tool.Name, in.Target)}, {now.Add(time.Second).Format("15:04:05"), "info", "正在采集诊断数据"}, {now.Add(2 * time.Second).Format("15:04:05"), "success", map[string]string{"ping": "响应延迟 8.4 ms，丢包率 0%", "tcp": "端口连接成功", "host-health": "CPU 38%，内存 62%，磁盘 71%", "k8s-pod": "Pod Ready，最近无异常事件", "log-search": "找到 28 条匹配日志"}[tool.ID]}}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := Result{fmt.Sprintf("diag-%03d", s.next), tool.ID, tool.Name, in.Target, "success", in.Operator, now.Format("15:04"), "2.0s", "诊断完成，未发现阻断性问题", lines}
	s.next++
	s.history = append([]Result{r}, s.history...)
	return r, nil
}
