package tickets

import (
	"errors"
	"testing"
)

func TestCreateApproveAndClose(t *testing.T) {
	service := NewService()
	item, err := service.Create(CreateInput{
		Title: "扩容订单服务", Type: "change", Priority: "high", Applicant: "admin",
		Description: "新增两个实例", RelatedAsset: "service-order", ProjectGroup: "订单平台",
		AutomationTemplate: "tpl-restart",
	})
	if err != nil || item.Status != StatusPending || item.RelatedAsset != "service-order" {
		t.Fatalf("create: %#v %v", item, err)
	}
	item, err = service.Approve(item.ID, "reviewer", "审批通过")
	if err != nil || item.Status != StatusApproved || item.Steps[1].Status != "completed" {
		t.Fatalf("approve: %#v %v", item, err)
	}
	item, err = service.Close(item.ID, "operator", "交付完成")
	if err != nil || item.Status != StatusCompleted || item.Steps[2].Status != "completed" {
		t.Fatalf("close: %#v %v", item, err)
	}
}

func TestRejectRequiresPending(t *testing.T) {
	service := NewService()
	item, err := service.Create(CreateInput{Title: "数据库申请", Type: "resource", Priority: "medium", Applicant: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Reject(item.ID, "reviewer", "信息不足"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Approve(item.ID, "reviewer", "重复审批"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected invalid state, got %v", err)
	}
}

func TestAlertTicketDeduplicatesAndResolves(t *testing.T) {
	service := NewService()
	input := AlertInput{ID: "fp-alert-1", Title: "主机不可用", Severity: "critical", Status: "firing", Target: "172.28.69.161", Message: "节点探针失败", Owner: "平台组"}
	first, created, err := service.SyncAlert(input)
	if err != nil || !created || first.RelatedAlert != input.ID || first.Type != "incident" {
		t.Fatalf("first alert ticket: %#v created=%v err=%v", first, created, err)
	}
	second, created, err := service.SyncAlert(input)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("duplicate alert ticket: %#v created=%v err=%v", second, created, err)
	}
	resolved, created, err := service.SyncAlert(AlertInput{ID: input.ID, Title: input.Title, Status: "resolved"})
	if err != nil || created || resolved.Status != StatusCompleted {
		t.Fatalf("resolved alert ticket: %#v created=%v err=%v", resolved, created, err)
	}
}

func TestCommentAndSummary(t *testing.T) {
	service := NewService()
	item, err := service.Create(CreateInput{Title: "访问申请", Type: "access", Priority: "low", Applicant: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	item, err = service.Comment(item.ID, "reviewer", "请补充用途")
	if err != nil || len(item.Timeline) < 2 {
		t.Fatalf("comment: %#v %v", item, err)
	}
	summary, err := service.Summary()
	if err != nil || summary.Total == 0 || summary.Pending == 0 {
		t.Fatalf("summary: %#v %v", summary, err)
	}
}

func TestTicketLinksTaskAndRelease(t *testing.T) {
	service := NewService()
	item, err := service.Create(CreateInput{Title: "关联交付", Type: "change", Priority: "medium", Applicant: "tester", RelatedTask: "job-1", RelatedRelease: "release-1"})
	if err != nil {
		t.Fatal(err)
	}
	if item.RelatedTask != "job-1" || item.RelatedRelease != "release-1" {
		t.Fatalf("ticket links not stored: %#v", item)
	}
}
