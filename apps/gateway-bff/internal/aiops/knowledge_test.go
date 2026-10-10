package aiops

import "testing"

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
	updated, err := service.UpdateKnowledge(created.ID, KnowledgeInput{Title: created.Title, Category: created.Category, Summary: created.Summary, Content: created.Content, Tags: created.Tags, Status: "published"}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "published" || len(service.SearchKnowledge("unique")) != 1 {
		t.Fatalf("unexpected published knowledge: %#v", updated)
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
