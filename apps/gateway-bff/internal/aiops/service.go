package aiops

import (
	"cmdb/gateway-bff/internal/demo"
	"errors"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found")
var ErrValidation = errors.New("validation")

type Evidence struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Weight int    `json:"weight"`
}
type Recommendation struct {
	RunbookID string   `json:"runbookId"`
	Title     string   `json:"title"`
	Risk      string   `json:"risk"`
	Impact    string   `json:"impact"`
	Duration  string   `json:"duration"`
	Steps     []string `json:"steps"`
}
type Analysis struct {
	ID              string           `json:"id"`
	AlertID         string           `json:"alertId"`
	Title           string           `json:"title"`
	Status          string           `json:"status"`
	Severity        string           `json:"severity"`
	Target          string           `json:"target"`
	RootCause       string           `json:"rootCause"`
	Confidence      int              `json:"confidence"`
	Summary         string           `json:"summary"`
	CreatedAt       string           `json:"createdAt"`
	Evidence        []Evidence       `json:"evidence"`
	Recommendations []Recommendation `json:"recommendations"`
}
type Knowledge struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Category string   `json:"category"`
	Summary  string   `json:"summary"`
	Tags     []string `json:"tags"`
	Helpful  int      `json:"helpful"`
}
type Execution struct {
	ID               string `json:"id"`
	AnalysisID       string `json:"analysisId"`
	RunbookID        string `json:"runbookId"`
	Status           string `json:"status"`
	RequestedBy      string `json:"requestedBy"`
	RequestedAt      string `json:"requestedAt"`
	ApprovalRequired bool   `json:"approvalRequired"`
}
type Summary struct {
	OpenAnomalies  int     `json:"openAnomalies"`
	AnalyzedToday  int     `json:"analyzedToday"`
	Accuracy       float64 `json:"accuracy"`
	KnowledgeCount int     `json:"knowledgeCount"`
	PendingActions int     `json:"pendingActions"`
}
type Service struct {
	mu         sync.RWMutex
	analyses   []Analysis
	knowledge  []Knowledge
	executions []Execution
}

func NewService() *Service {
	if !demo.Enabled() {
		return &Service{analyses: []Analysis{}, knowledge: []Knowledge{{ID: "kb-001", Title: "Java 服务 OOM 排查手册", Category: "故障处理", Summary: "从堆转储、GC 日志和容器限制定位内存异常", Tags: []string{"OOM", "Java", "内存"}, Helpful: 48}, {ID: "kb-002", Title: "数据库连接池耗尽处理", Category: "数据库", Summary: "识别慢查询与连接泄漏并安全恢复", Tags: []string{"MySQL", "连接池", "延迟"}, Helpful: 36}, {ID: "kb-003", Title: "Kubernetes Pod 重启诊断", Category: "Kubernetes", Summary: "分析 OOMKilled、探针失败和节点驱逐", Tags: []string{"Pod", "OOM", "K8s"}, Helpful: 29}}, executions: []Execution{}}
	}
	rec := Recommendation{"rb-memory-relief", "支付网关内存缓解", "medium", "滚动重启单个实例，流量自动摘除", "约 4 分钟", []string{"确认当前实例连接数", "从负载均衡摘除目标实例", "生成堆转储并归档", "滚动重启并执行健康检查", "恢复流量并观察 5 分钟"}}
	return &Service{analyses: []Analysis{{"ana-001", "alert-001", "支付网关内存异常分析", "completed", "critical", "payment-gateway-vm", "应用缓存对象未及时释放", 92, "内存增长与 v4.1.0 发布高度相关，未发现宿主机资源竞争。", "14:28", []Evidence{{"metric", "内存趋势突增", "发布后 18 分钟由 62% 上升至 91.8%", 35}, {"change", "近期版本变更", "支付网关 v4.1.0 于 12:35 发布", 30}, {"topology", "影响范围", "仅 payment-gateway-vm 异常，同集群节点正常", 20}, {"log", "日志特征", "重复出现 cache allocation slow 警告", 15}}, []Recommendation{rec}}, {"ana-002", "alert-002", "订单接口延迟归因", "completed", "warning", "service-order", "数据库连接池等待时间增加", 86, "慢查询导致连接占用时间延长，接口 P95 随之升高。", "14:31", []Evidence{{"metric", "连接池使用率", "当前 84%，高于历史基线", 40}, {"log", "慢查询日志", "近 15 分钟出现 12 条超过 2 秒查询", 35}}, []Recommendation{{"rb-db-slow-query", "数据库慢查询缓解", "low", "终止异常查询并刷新统计信息", "约 3 分钟", []string{"导出慢查询列表", "确认业务影响", "终止异常查询", "刷新统计信息"}}}}}, knowledge: []Knowledge{{"kb-001", "Java 服务 OOM 排查手册", "故障处理", "从堆转储、GC 日志和容器限制定位内存异常", []string{"OOM", "Java", "内存"}, 48}, {"kb-002", "数据库连接池耗尽处理", "数据库", "识别慢查询与连接泄漏并安全恢复", []string{"MySQL", "连接池", "延迟"}, 36}, {"kb-003", "Kubernetes Pod 重启诊断", "Kubernetes", "分析 OOMKilled、探针失败和节点驱逐", []string{"Pod", "OOM", "K8s"}, 29}}}
}
func (s *Service) Summary() Summary {
	if !demo.Enabled() {
		return Summary{KnowledgeCount: len(s.knowledge)}
	}
	return Summary{2, 2, 91.6, len(s.knowledge), len(s.executions)}
}
func (s *Service) Analyses() []Analysis { return append([]Analysis(nil), s.analyses...) }
func (s *Service) Analyze(alertID string) (Analysis, error) {
	for _, a := range s.analyses {
		if a.AlertID == alertID {
			return a, nil
		}
	}
	return Analysis{}, ErrNotFound
}
func (s *Service) Knowledge() []Knowledge { return append([]Knowledge(nil), s.knowledge...) }
func (s *Service) SearchKnowledge(q string) []Knowledge {
	q = strings.ToLower(q)
	var out []Knowledge
	for _, k := range s.knowledge {
		hay := strings.ToLower(k.Title + " " + k.Summary + " " + strings.Join(k.Tags, " "))
		if q == "" || strings.Contains(hay, q) {
			out = append(out, k)
		}
	}
	return out
}
func (s *Service) Executions() []Execution {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Execution(nil), s.executions...)
}
func (s *Service) Execute(analysisID, runbookID, user string) (Execution, error) {
	if analysisID == "" || runbookID == "" {
		return Execution{}, ErrValidation
	}
	if _, err := s.find(analysisID); err != nil {
		return Execution{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	x := Execution{"exec-" + time.Now().Format("150405"), analysisID, runbookID, "pending-approval", user, time.Now().Format("15:04"), true}
	s.executions = append([]Execution{x}, s.executions...)
	return x, nil
}
func (s *Service) find(id string) (Analysis, error) {
	for _, a := range s.analyses {
		if a.ID == id {
			return a, nil
		}
	}
	return Analysis{}, ErrNotFound
}
