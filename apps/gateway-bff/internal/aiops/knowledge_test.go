package aiops

import (
	"context"
	"strings"
	"testing"
)

type fakeKnowledgeStore struct{ data map[string][]byte }

func (f *fakeKnowledgeStore) Put(_ context.Context, key string, content []byte, _ string) (string, error) {
	if f.data == nil {
		f.data = map[string][]byte{}
	}
	f.data[key] = append([]byte(nil), content...)
	return "etag", nil
}
func (f *fakeKnowledgeStore) Get(_ context.Context, key string) ([]byte, error) {
	return append([]byte(nil), f.data[key]...), nil
}
func (f *fakeKnowledgeStore) Delete(_ context.Context, key string) error {
	delete(f.data, key)
	return nil
}
func (f *fakeKnowledgeStore) Bucket() string { return "test" }

func TestKnowledgeCRUDAndAnalysisSynthesis(t *testing.T) {
	service := NewService()
	created, err := service.CreateKnowledge(KnowledgeInput{Title: "unique knowledge title", Category: "测试", Summary: "summary", Content: "content", Tags: []string{"测试"}, Status: "draft"}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if len(service.SearchKnowledge("unique")) != 0 {
		t.Fatal("draft knowledge must not be returned to normal search")
	}
	if len(service.SearchKnowledgeAll("unique")) != 1 {
		t.Fatal("management search must return draft knowledge")
	}
	updated, err := service.UpdateKnowledge(created.ID, KnowledgeInput{Title: created.Title, Category: created.Category, Summary: created.Summary, Content: "content-v2", Tags: created.Tags, Status: "published"}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "published" || len(service.SearchKnowledge("unique")) != 1 {
		t.Fatalf("unexpected published knowledge: %#v", updated)
	}
	versions, err := service.KnowledgeVersions(created.ID)
	if err != nil || len(versions) != 2 || versions[0].Version != 2 {
		t.Fatalf("unexpected knowledge versions: %#v %v", versions, err)
	}
	restored, err := service.RestoreKnowledge(created.ID, 1, "tester")
	if err != nil || restored.Content != "content" {
		t.Fatalf("restore knowledge failed: %#v %v", restored, err)
	}
	afterRestore, err := service.KnowledgeVersions(created.ID)
	if err != nil || len(afterRestore) != 3 {
		t.Fatalf("restore did not create a version: %#v %v", afterRestore, err)
	}
	helpful, err := service.MarkKnowledgeHelpful(created.ID)
	if err != nil || helpful.Helpful != 1 {
		t.Fatalf("mark helpful failed: %#v %v", helpful, err)
	}

	service.mu.Lock()
	service.analyses = append(service.analyses, Analysis{ID: "analysis-kb-1", Title: "磁盘告警复盘", Summary: "磁盘使用率持续升高", RootCause: "磁盘空间耗尽风险", Severity: "warning", Target: "node-1", Recommendations: []Recommendation{{Steps: []string{"检查磁盘", "清理日志"}}}})
	service.mu.Unlock()
	fromAnalysis, err := service.CreateKnowledgeFromAnalysis("analysis-kb-1", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if fromAnalysis.Source != "analysis" || fromAnalysis.SourceID != "analysis-kb-1" || fromAnalysis.Status != "draft" {
		t.Fatalf("unexpected synthesized knowledge: %#v", fromAnalysis)
	}
}

func TestKnowledgeSuggestionsPreferTitleAndTags(t *testing.T) {
	service := NewService()
	items := service.knowledgeSuggestionsFor("磁盘空间耗尽风险", "磁盘使用率告警", 3)
	if len(items) == 0 || items[0].ID != "kb-005" {
		t.Fatalf("expected disk knowledge suggestion, got %#v", items)
	}
}

func TestKnowledgeEmbeddingAndHybridSearch(t *testing.T) {
	left := embedKnowledgeText("磁盘空间清理")
	right := embedKnowledgeText("磁盘空间不足")
	if similarity := cosineSimilarity(left, right); similarity < 0.4 {
		t.Fatalf("expected similar phrases to share vector space, got %.3f", similarity)
	}
	service := NewService()
	items := service.SearchKnowledgeHybrid("磁盘空间清理", false)
	if len(items) == 0 || items[0].ID != "kb-005" {
		t.Fatalf("expected hybrid search to rank disk knowledge first, got %#v", items)
	}
}

func TestAskKnowledgeReturnsCitedAnswer(t *testing.T) {
	service := NewService()
	answer, err := service.AskKnowledge("磁盘空间不足应该怎么处理", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(answer.Citations) == 0 || answer.Citations[0].KnowledgeID != "kb-005" || !strings.Contains(answer.Answer, "[kb-005]") {
		t.Fatalf("unexpected knowledge answer: %#v", answer)
	}
	if _, err = service.AskKnowledge("   ", 3); err == nil {
		t.Fatal("expected empty question to be rejected")
	}
	empty, err := service.AskKnowledge("完全不存在的量子网络故障", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Citations) != 0 {
		t.Fatalf("expected unrelated question to return no citations, got %#v", empty.Citations)
	}
}

func TestMaskSensitiveKnowledgeText(t *testing.T) {
	value := "password=Secret123 token: abcdef Authorization: Bearer xyz.123"
	masked := maskSensitiveText(value)
	if strings.Contains(masked, "Secret123") || strings.Contains(masked, "abcdef") || strings.Contains(masked, "xyz.123") {
		t.Fatalf("sensitive values were not masked: %s", masked)
	}
}

func TestKnowledgeAttachmentLifecycle(t *testing.T) {
	service := NewService()
	store := &fakeKnowledgeStore{data: map[string][]byte{}}
	service.objectStore = store
	created, err := service.CreateKnowledge(KnowledgeInput{Title: "attachment knowledge", Category: "测试", Summary: "summary", Content: "content", Status: "published"}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	item, err := service.UploadKnowledgeAttachment(context.Background(), created.ID, "runbook.txt", "text/plain", []byte("payload"), "tester")
	if err != nil || item.Filename != "runbook.txt" || item.Size != 7 {
		t.Fatalf("upload attachment failed: %#v %v", item, err)
	}
	items, err := service.KnowledgeAttachments(created.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("list attachments failed: %#v %v", items, err)
	}
	_, content, err := service.DownloadKnowledgeAttachment(context.Background(), created.ID, item.ID)
	if err != nil || string(content) != "payload" {
		t.Fatalf("download attachment failed: %q %v", content, err)
	}
	if err = service.DeleteKnowledgeAttachment(context.Background(), created.ID, item.ID); err != nil {
		t.Fatal(err)
	}
	if len(store.data) != 0 {
		t.Fatalf("object was not deleted: %#v", store.data)
	}
}
