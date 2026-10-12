package aiops

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

const knowledgeRAGMinVectorScore = 0.35

func (s *Service) AskKnowledge(question string, limit int) (KnowledgeAnswer, error) {
	return s.AskKnowledgeWithUser(question, limit, "system")
}

func (s *Service) AskKnowledgeWithUser(question string, limit int, user string) (KnowledgeAnswer, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return KnowledgeAnswer{}, ErrValidation
	}
	question = maskSensitiveText(question)
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	_ = s.reloadKnowledge()
	s.mu.RLock()
	byID := make(map[string]Knowledge, len(s.knowledge))
	for _, item := range s.knowledge {
		if item.Status == "published" {
			byID[item.ID] = item
		}
	}
	s.mu.RUnlock()

	type scoredKnowledge struct {
		item  Knowledge
		score float64
	}
	scores := map[string]*scoredKnowledge{}
	for index, item := range s.searchKnowledge(question, false) {
		if _, ok := byID[item.ID]; !ok {
			continue
		}
		scores[item.ID] = &scoredKnowledge{item: item, score: float64(len(byID)-index) * 1.5}
	}
	queryVector, _ := s.embedText(context.Background(), question)
	matches, _ := s.repo.SearchKnowledgeVector(context.Background(), queryVector, 30)
	for _, match := range matches {
		if match.Score < knowledgeRAGMinVectorScore {
			continue
		}
		item, ok := byID[match.KnowledgeID]
		if !ok {
			continue
		}
		if existing, ok := scores[item.ID]; ok {
			existing.score += match.Score * 10
		} else {
			scores[item.ID] = &scoredKnowledge{item: item, score: match.Score * 10}
		}
	}
	adjustments := map[string]float64{}
	if answers, err := s.repo.LoadKnowledgeAnswers(context.Background(), 1000); err == nil {
		for _, answer := range answers {
			if answer.Feedback == nil {
				continue
			}
			delta := 0.0
			switch answer.Feedback.Status {
			case "helpful":
				delta = 0.5
			case "not-helpful":
				delta = -1.5
			case "corrected":
				delta = -2.0
			}
			for _, citation := range answer.Citations {
				adjustments[citation.KnowledgeID] += delta
			}
		}
	}
	for id, item := range scores {
		item.score += adjustments[id]
	}
	ranked := make([]scoredKnowledge, 0, len(scores))
	for _, item := range scores {
		ranked = append(ranked, *item)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].item.Helpful > ranked[j].item.Helpful
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	answer := KnowledgeAnswer{ID: newID("kans"), Question: question, Citations: []KnowledgeCitation{}, GeneratedAt: time.Now().Format("2006-01-02 15:04:05"), Model: s.EmbeddingModel(), CreatedBy: strings.TrimSpace(user)}
	if len(ranked) == 0 {
		answer.Answer = "知识库中暂未找到与该问题直接相关的已发布知识，建议补充故障现象、组件名称或错误关键词后重试。"
		_ = s.repo.SaveKnowledgeAnswer(context.Background(), answer)
		return answer, nil
	}
	var builder strings.Builder
	builder.WriteString("根据知识库检索结果，建议优先参考以下排障知识：\n")
	for index, item := range ranked {
		snippet := knowledgeSnippet(item.item, question)
		citation := KnowledgeCitation{KnowledgeID: item.item.ID, Title: item.item.Title, Category: item.item.Category, Snippet: snippet, Score: item.score, UpdatedAt: item.item.UpdatedAt}
		answer.Citations = append(answer.Citations, citation)
		builder.WriteString(fmt.Sprintf("%d. 《%s》（%s）：%s [%s]\n", index+1, item.item.Title, item.item.Category, snippet, item.item.ID))
	}
	builder.WriteString("以上结论来自知识库检索，请结合当前告警、监控指标和变更记录确认后执行。")
	answer.Answer = maskSensitiveText(builder.String())
	_ = s.repo.SaveKnowledgeAnswer(context.Background(), answer)
	return answer, nil
}

func (s *Service) SubmitKnowledgeAnswerFeedback(ctx context.Context, answerID string, input KnowledgeAnswerFeedback) (KnowledgeAnswer, error) {
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.Status != "helpful" && input.Status != "not-helpful" && input.Status != "corrected" {
		return KnowledgeAnswer{}, ErrValidation
	}
	item, err := s.repo.GetKnowledgeAnswer(ctx, answerID)
	if err != nil {
		return KnowledgeAnswer{}, err
	}
	input.CorrectedAnswer = maskSensitiveText(strings.TrimSpace(input.CorrectedAnswer))
	input.Note = maskSensitiveText(strings.TrimSpace(input.Note))
	input.Reviewer = strings.TrimSpace(input.Reviewer)
	input.ReviewedAt = time.Now().Format("2006-01-02 15:04:05")
	item.Feedback = &input
	if err = s.repo.SaveKnowledgeAnswer(ctx, item); err != nil {
		return KnowledgeAnswer{}, err
	}
	if input.Status == "corrected" && input.CorrectedAnswer != "" {
		_, _ = s.CreateKnowledge(KnowledgeInput{Title: "RAG 纠正：" + item.Question, Category: "故障处理", Summary: item.Answer, Content: input.CorrectedAnswer, Tags: []string{"RAG修正", item.ID}, Status: "draft"}, input.Reviewer)
	}
	return item, nil
}

func (s *Service) KnowledgeAnswerFeedbackStats(ctx context.Context) (map[string]int, error) {
	items, err := s.repo.LoadKnowledgeAnswers(ctx, 1000)
	if err != nil {
		return nil, err
	}
	result := map[string]int{"total": len(items), "helpful": 0, "notHelpful": 0, "corrected": 0, "pending": 0}
	for _, item := range items {
		if item.Feedback == nil {
			result["pending"]++
			continue
		}
		switch item.Feedback.Status {
		case "helpful":
			result["helpful"]++
		case "not-helpful":
			result["notHelpful"]++
		case "corrected":
			result["corrected"]++
		}
	}
	return result, nil
}

func knowledgeSnippet(item Knowledge, question string) string {
	text := strings.TrimSpace(item.Summary)
	if strings.TrimSpace(item.Content) != "" {
		if text != "" {
			text += "。"
		}
		text += strings.TrimSpace(item.Content)
	}
	if text == "" {
		return "暂无摘要"
	}
	queryTokens := map[string]bool{}
	for _, token := range knowledgeTokens(question) {
		queryTokens[token] = true
	}
	parts := strings.FieldsFunc(text, func(r rune) bool { return r == '。' || r == '！' || r == '？' || r == '\n' || r == '；' })
	best := ""
	bestScore := -1
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		score := 0
		for _, token := range knowledgeTokens(part) {
			if queryTokens[token] {
				score++
			}
		}
		if score > bestScore {
			best, bestScore = part, score
		}
	}
	if best == "" {
		best = text
	}
	runes := []rune(best)
	if len(runes) > 180 {
		return string(runes[:180]) + "…"
	}
	return best
}
