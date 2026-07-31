package releases

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrNotFound = errors.New("release not found")
var ErrValidation = errors.New("validation")

type Stage struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Duration string `json:"duration"`
	Message  string `json:"message"`
}
type Release struct {
	ID              string  `json:"id"`
	Application     string  `json:"application"`
	Version         string  `json:"version"`
	PreviousVersion string  `json:"previousVersion"`
	Environment     string  `json:"environment"`
	Status          string  `json:"status"`
	Progress        int     `json:"progress"`
	Operator        string  `json:"operator"`
	StartedAt       string  `json:"startedAt"`
	Strategy        string  `json:"strategy"`
	Approval        string  `json:"approval"`
	TicketID        string  `json:"ticketId"`
	Stages          []Stage `json:"stages"`
}
type Artifact struct {
	ID          string `json:"id"`
	Application string `json:"application"`
	Version     string `json:"version"`
	Commit      string `json:"commit"`
	Size        string `json:"size"`
	CreatedAt   string `json:"createdAt"`
	Security    string `json:"security"`
}
type StartInput struct {
	Application string `json:"application"`
	Version     string `json:"version"`
	Environment string `json:"environment"`
	Operator    string `json:"operator"`
}
type Summary struct {
	Today       int     `json:"today"`
	Running     int     `json:"running"`
	Success     int     `json:"success"`
	Failed      int     `json:"failed"`
	SuccessRate float64 `json:"successRate"`
}
type Service struct {
	mu        sync.RWMutex
	releases  []Release
	artifacts []Artifact
	next      int
}

func stages(running bool) []Stage {
	return []Stage{{"build", "构建制品", "success", "01:42", "镜像构建完成"}, {"scan", "安全扫描", "success", "00:38", "未发现高危漏洞"}, {"approve", "发布门禁", "success", "00:12", "审批与变更窗口已校验"}, {"deploy", "灰度部署", map[bool]string{true: "running", false: "success"}[running], "02:16", "部署 2/4 实例"}, {"verify", "指标验证", map[bool]string{true: "pending", false: "success"}[running], "", "等待执行"}}
}
func NewService() *Service {
	return &Service{releases: []Release{{"rel-001", "订单服务", "v2.7.4", "v2.7.3", "production", "running", 72, "王强", "14:26", "灰度发布", "approved", "TKT-20260715-001", stages(true)}, {"rel-002", "数据分析 Worker", "v1.9.2", "v1.9.1", "production", "success", 100, "陈明", "13:18", "滚动发布", "approved", "TKT-20260715-006", stages(false)}, {"rel-003", "支付网关", "v4.1.0", "v4.0.8", "production", "failed", 64, "周敏", "12:35", "蓝绿发布", "approved", "TKT-20260715-003", []Stage{{"build", "构建制品", "success", "01:10", "完成"}, {"scan", "安全扫描", "success", "00:31", "通过"}, {"deploy", "部署新版本", "failed", "01:24", "健康检查失败"}}}}, artifacts: []Artifact{{"art-01", "订单服务", "v2.7.4", "8ac91f2", "286 MB", "14:20", "passed"}, {"art-02", "支付网关", "v4.1.0", "31bd808", "194 MB", "12:30", "passed"}, {"art-03", "数据分析 Worker", "v1.9.2", "fa209cb", "412 MB", "13:10", "passed"}}, next: 4}
}
func (s *Service) List() []Release {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Release(nil), s.releases...)
}
func (s *Service) Artifacts() []Artifact { return append([]Artifact(nil), s.artifacts...) }
func (s *Service) Summary() Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r := Summary{Today: len(s.releases), SuccessRate: 96.8}
	for _, x := range s.releases {
		if x.Status == "running" || x.Status == "rolling-back" {
			r.Running++
		}
		if x.Status == "success" {
			r.Success++
		}
		if x.Status == "failed" {
			r.Failed++
		}
	}
	return r
}
func (s *Service) Start(in StartInput) (Release, error) {
	if in.Application == "" || in.Version == "" || in.Environment == "" {
		return Release{}, ErrValidation
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	x := Release{ID: fmt.Sprintf("rel-%03d", s.next), Application: in.Application, Version: in.Version, PreviousVersion: "v2.7.4", Environment: in.Environment, Status: "running", Progress: 20, Operator: in.Operator, StartedAt: time.Now().Format("15:04"), Strategy: "灰度发布", Approval: "approved", Stages: stages(true)}
	s.next++
	s.releases = append([]Release{x}, s.releases...)
	return x, nil
}
func (s *Service) Rollback(id, operator string) (Release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.releases {
		if s.releases[i].ID == id {
			s.releases[i].Status = "rolling-back"
			s.releases[i].Operator = operator
			s.releases[i].Progress = 15
			s.releases[i].Stages = append(s.releases[i].Stages, Stage{"rollback", "回滚至 " + s.releases[i].PreviousVersion, "running", "", "正在恢复流量"})
			return s.releases[i], nil
		}
	}
	return Release{}, ErrNotFound
}
