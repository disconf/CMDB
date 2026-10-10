package aiops

import (
	"context"
	"sort"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/jobs"
)

func (s *Service) CanaryValidations() []CanaryValidation {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if items, err := s.repo.LoadCanaryValidations(ctx); err == nil {
		for i := range items {
			if items[i].Status != "running" || strings.TrimSpace(items[i].JobID) == "" || s.jobs == nil {
				continue
			}
			job, jobErr := s.jobs.Get(items[i].JobID)
			if jobErr != nil || (job.Status != "success" && job.Status != "failed" && job.Status != "cancelled") {
				continue
			}
			items[i] = s.completeCanary(items[i], job)
			_ = s.repo.SaveCanaryValidation(ctx, items[i])
		}
		s.mu.Lock()
		s.canaries = items
		s.mu.Unlock()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := append([]CanaryValidation(nil), s.canaries...)
	sort.SliceStable(result, func(i, j int) bool { return result[i].StartedAt > result[j].StartedAt })
	return result
}

func (s *Service) StartCanaryValidation(ctx context.Context, runbookID, assetID, user string) (CanaryValidation, error) {
	runbookID, assetID, user = strings.TrimSpace(runbookID), strings.TrimSpace(assetID), strings.TrimSpace(user)
	if runbookID == "" || assetID == "" || user == "" {
		return CanaryValidation{}, ErrValidation
	}
	policy, ok := s.policyFor(runbookID)
	if !ok || policy.Mode != "change" {
		return CanaryValidation{}, ErrValidation
	}
	if s.assets == nil || s.jobs == nil {
		return CanaryValidation{}, ErrNotReady
	}
	asset, err := s.assets.GetAsset(assetID)
	if err != nil {
		return CanaryValidation{}, ErrValidation
	}
	found := false
	for _, template := range s.jobs.Templates() {
		if template.ID == "tpl-health" && template.Enabled {
			found = true
			break
		}
	}
	if !found {
		return CanaryValidation{}, ErrNotReady
	}
	job, err := s.jobs.Create(jobs.CreateInput{TemplateID: "tpl-health", Targets: []string{assetID}, Operator: "aiops:canary:" + user, Parameters: map[string]string{"aiops_runbook_id": runbookID, "canary": "true"}, PreApproved: true})
	if err != nil {
		return CanaryValidation{}, err
	}
	job, err = s.jobs.Run(job.ID)
	if err != nil {
		return CanaryValidation{}, err
	}
	item := CanaryValidation{ID: newID("canary"), RunbookID: runbookID, AssetID: assetID, AssetName: asset.Name, Status: "running", JobID: job.ID, Summary: "只读健康检查任务已启动", Checks: []string{"主线任务服务可用", "目标资产可解析", "SSH/Runner 通道待任务结果确认", "检查过程不得修改主机"}, RequestedBy: user, StartedAt: time.Now().Format("2006-01-02 15:04:05")}
	if job.Status == "success" || job.Status == "failed" {
		item = s.completeCanary(item, job)
	}
	if err = s.repo.SaveCanaryValidation(ctx, item); err != nil {
		return CanaryValidation{}, err
	}
	s.mu.Lock()
	s.canaries = append([]CanaryValidation{item}, s.canaries...)
	s.mu.Unlock()
	return item, nil
}

func (s *Service) completeCanaryFromJob(job jobs.Job) {
	items := s.CanaryValidations()
	index := -1
	for i := range items {
		if items[i].JobID == job.ID {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	item := items[index]
	item = s.completeCanary(item, job)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.repo.SaveCanaryValidation(ctx, item); err != nil {
		return
	}
	s.mu.Lock()
	for i := range s.canaries {
		if s.canaries[i].ID == item.ID {
			s.canaries[i] = item
			break
		}
	}
	s.mu.Unlock()
}

func (s *Service) completeCanary(item CanaryValidation, job jobs.Job) CanaryValidation {
	item.FinishedAt = time.Now().Format("2006-01-02 15:04:05")
	changed := false
	for _, result := range job.TargetResults {
		if result.Changed {
			changed = true
		}
	}
	switch {
	case job.Status == "success" && !changed:
		item.Status = "passed"
		item.Summary = "只读健康检查完成，主机、Runner 和凭据通道可用"
	case job.Status == "success" && changed:
		item.Status = "failed"
		item.Summary = "健康检查产生了变更，灰度通道验证失败"
	default:
		item.Status = "failed"
		item.Summary = "健康检查任务未成功完成"
	}
	return item
}

func (s *Service) hasRecentPassedCanary(runbookID, assetID string, within time.Duration) bool {
	for _, item := range s.CanaryValidations() {
		if item.RunbookID != runbookID || item.AssetID != assetID || item.Status != "passed" {
			continue
		}
		started, err := parseAITime(item.StartedAt)
		if err == nil && time.Since(started) <= within {
			return true
		}
	}
	return false
}
