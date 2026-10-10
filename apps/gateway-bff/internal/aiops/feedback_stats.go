package aiops

import (
	"math"
	"sort"
	"strings"
)

func (s *Service) FeedbackStats() FeedbackStats {
	s.mu.RLock()
	items := append([]Analysis(nil), s.analyses...)
	s.mu.RUnlock()
	result := FeedbackStats{ByRootCause: []RootCauseFeedbackStat{}}
	byRoot := map[string]*RootCauseFeedbackStat{}
	for _, item := range items {
		if item.Feedback == nil {
			continue
		}
		result.Total++
		rootCause := strings.TrimSpace(item.RootCause)
		if rootCause == "" {
			rootCause = "待进一步确认的异常"
		}
		stat := byRoot[rootCause]
		if stat == nil {
			stat = &RootCauseFeedbackStat{RootCause: rootCause}
			byRoot[rootCause] = stat
		}
		switch item.Feedback.Status {
		case "confirmed":
			result.Confirmed++
			stat.Confirmed++
		case "rejected":
			result.Rejected++
			stat.Rejected++
		case "partial":
			result.Partial++
			stat.Partial++
		}
	}
	for _, stat := range byRoot {
		completed := stat.Confirmed + stat.Rejected
		if completed > 0 {
			stat.Accuracy = math.Round(float64(stat.Confirmed)/float64(completed)*10000) / 100
		}
		stat.Adjustment = adjustmentFor(stat.Confirmed, stat.Rejected, stat.Partial)
		result.ByRootCause = append(result.ByRootCause, *stat)
	}
	completed := result.Confirmed + result.Rejected
	if completed > 0 {
		result.Accuracy = math.Round(float64(result.Confirmed)/float64(completed)*10000) / 100
	}
	sort.SliceStable(result.ByRootCause, func(i, j int) bool {
		left, right := result.ByRootCause[i], result.ByRootCause[j]
		leftTotal := left.Confirmed + left.Rejected + left.Partial
		rightTotal := right.Confirmed + right.Rejected + right.Partial
		if leftTotal != rightTotal {
			return leftTotal > rightTotal
		}
		return left.RootCause < right.RootCause
	})
	return result
}

func (s *Service) feedbackAdjustment(alertTitle, rootCause string) (string, int, string, int) {
	s.mu.RLock()
	items := append([]Analysis(nil), s.analyses...)
	s.mu.RUnlock()
	corrected := ""
	correctedAt := ""
	title := strings.ToLower(strings.TrimSpace(alertTitle))
	for _, item := range items {
		if item.Feedback == nil || item.Feedback.CorrectedRootCause == "" {
			continue
		}
		if strings.ToLower(strings.TrimSpace(item.AlertTitle)) != title {
			continue
		}
		if item.Feedback.ReviewedAt >= correctedAt {
			corrected = item.Feedback.CorrectedRootCause
			correctedAt = item.Feedback.ReviewedAt
		}
	}
	if corrected != "" {
		rootCause = corrected
	}
	confirmed, rejected, partial := 0, 0, 0
	for _, item := range items {
		if item.Feedback == nil || item.RootCause != rootCause {
			continue
		}
		switch item.Feedback.Status {
		case "confirmed":
			confirmed++
		case "rejected":
			rejected++
		case "partial":
			partial++
		}
	}
	adjustment := adjustmentFor(confirmed, rejected, partial)
	total := confirmed + rejected + partial
	reason := ""
	if corrected != "" {
		reason = "已应用历史修正根因：" + corrected
	}
	if total > 0 {
		if reason != "" {
			reason += "；"
		}
		reason += "历史反馈已纳入权重"
	}
	return rootCause, adjustment, reason, total
}

func adjustmentFor(confirmed, rejected, partial int) int {
	completed := confirmed + rejected
	if completed < 3 {
		return 0
	}
	accuracy := float64(confirmed) / float64(completed) * 100
	adjustment := int(math.Round((accuracy - 50) / 5))
	if partial > 0 {
		adjustment -= minInt(partial, 2)
	}
	if adjustment > 10 {
		return 10
	}
	if adjustment < -10 {
		return -10
	}
	return adjustment
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
