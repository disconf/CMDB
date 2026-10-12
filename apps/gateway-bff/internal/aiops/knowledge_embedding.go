package aiops

import (
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"unicode"
)

const (
	knowledgeEmbeddingModel = "local-hash-v1"
	knowledgeEmbeddingDim   = 256
)

func knowledgeEmbeddingText(item Knowledge) string {
	parts := []string{
		strings.Repeat(item.Title+" ", 3),
		strings.Repeat(item.Category+" ", 2),
		strings.Repeat(strings.Join(item.Tags, " ")+" ", 2),
		item.Summary,
		item.Content,
	}
	for _, ref := range item.Related {
		parts = append(parts, ref.Name)
	}
	return strings.Join(parts, "\n")
}

func embedKnowledgeText(value string) []float32 {
	vector := make([]float32, knowledgeEmbeddingDim)
	for _, token := range knowledgeTokens(value) {
		hasher := fnv.New64a()
		_, _ = hasher.Write([]byte(token))
		index := int(hasher.Sum64() % knowledgeEmbeddingDim)
		vector[index] += 1
	}
	var norm float64
	for _, value := range vector {
		norm += float64(value * value)
	}
	if norm == 0 {
		return vector
	}
	norm = math.Sqrt(norm)
	for index := range vector {
		vector[index] = float32(float64(vector[index]) / norm)
	}
	return vector
}

func knowledgeTokens(value string) []string {
	value = strings.ToLower(value)
	result := []string{}
	var latin strings.Builder
	var previousHan rune
	flush := func() {
		if latin.Len() > 0 {
			result = append(result, latin.String())
			latin.Reset()
		}
	}
	for _, r := range value {
		switch {
		case unicode.Is(unicode.Han, r):
			flush()
			result = append(result, string(r))
			if previousHan != 0 {
				result = append(result, string(previousHan)+string(r))
			}
			previousHan = r
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			previousHan = 0
			latin.WriteRune(r)
		default:
			previousHan = 0
			flush()
		}
	}
	flush()
	return result
}

func cosineSimilarity(left, right []float32) float64 {
	if len(left) == 0 || len(right) == 0 || len(left) != len(right) {
		return 0
	}
	var dot, leftNorm, rightNorm float64
	for index := range left {
		dot += float64(left[index] * right[index])
		leftNorm += float64(left[index] * left[index])
		rightNorm += float64(right[index] * right[index])
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

func mergeKnowledgeHybrid(lexical []Knowledge, vectorMatches []KnowledgeVectorMatch, byID map[string]Knowledge, limit int) []Knowledge {
	type scoreItem struct {
		item  Knowledge
		score float64
	}
	scores := map[string]*scoreItem{}
	for index, item := range lexical {
		scores[item.ID] = &scoreItem{item: item, score: float64(len(lexical)-index) * 1.5}
	}
	for _, match := range vectorMatches {
		item, ok := byID[match.KnowledgeID]
		if !ok {
			continue
		}
		if existing, ok := scores[item.ID]; ok {
			existing.score += match.Score * 10
		} else {
			scores[item.ID] = &scoreItem{item: item, score: match.Score * 10}
		}
	}
	result := make([]scoreItem, 0, len(scores))
	for _, item := range scores {
		result = append(result, *item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].score != result[j].score {
			return result[i].score > result[j].score
		}
		return result[i].item.Helpful > result[j].item.Helpful
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	merged := make([]Knowledge, 0, len(result))
	for _, item := range result {
		merged = append(merged, item.item)
	}
	return merged
}
