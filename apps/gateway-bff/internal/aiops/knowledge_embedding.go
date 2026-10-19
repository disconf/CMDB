package aiops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	knowledgeEmbeddingModel = "local-hash-v1"
	knowledgeEmbeddingDim   = 256
)

type knowledgeEmbeddingProvider interface {
	Name() string
	Dimensions() int
	Embed(context.Context, string) ([]float32, error)
}

type localKnowledgeEmbeddingProvider struct{}

func (localKnowledgeEmbeddingProvider) Name() string    { return knowledgeEmbeddingModel }
func (localKnowledgeEmbeddingProvider) Dimensions() int { return knowledgeEmbeddingDim }
func (localKnowledgeEmbeddingProvider) Embed(_ context.Context, value string) ([]float32, error) {
	return embedKnowledgeText(value), nil
}

type externalKnowledgeEmbeddingProvider struct {
	baseURL string
	apiKey  string
	model   string
	dims    int
	client  *http.Client
}

func newKnowledgeEmbeddingProvider() (knowledgeEmbeddingProvider, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("AI_EMBEDDING_BASE_URL")), "/")
	model := strings.TrimSpace(os.Getenv("AI_EMBEDDING_MODEL"))
	apiKey := strings.TrimSpace(os.Getenv("AI_EMBEDDING_API_KEY"))
	if baseURL == "" || model == "" {
		return localKnowledgeEmbeddingProvider{}, nil
	}
	dims := knowledgeEmbeddingDim
	if value := strings.TrimSpace(os.Getenv("AI_EMBEDDING_DIMENSIONS")); value != "" {
		if _, err := fmt.Sscanf(value, "%d", &dims); err != nil || dims != knowledgeEmbeddingDim {
			return nil, errors.New("AI_EMBEDDING_DIMENSIONS must be 256 with the current pgvector schema")
		}
	}
	return &externalKnowledgeEmbeddingProvider{baseURL: baseURL, apiKey: apiKey, model: model, dims: dims, client: &http.Client{Timeout: 20 * time.Second}}, nil
}

func (e *externalKnowledgeEmbeddingProvider) Name() string    { return e.model }
func (e *externalKnowledgeEmbeddingProvider) Dimensions() int { return e.dims }
func (e *externalKnowledgeEmbeddingProvider) Embed(ctx context.Context, value string) ([]float32, error) {
	payload, err := json.Marshal(map[string]any{"model": e.model, "input": value, "dimensions": e.dims})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+e.apiKey)
	}
	response, err := e.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var document struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err = json.NewDecoder(response.Body).Decode(&document); err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || len(document.Data) == 0 {
		return nil, fmt.Errorf("embedding provider returned %s", response.Status)
	}
	vector := document.Data[0].Embedding
	if len(vector) != e.dims {
		return nil, fmt.Errorf("embedding dimensions %d do not match schema %d", len(vector), e.dims)
	}
	return normalizeVector(vector), nil
}

func normalizeVector(vector []float32) []float32 {
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

func knowledgeEmbeddingText(item Knowledge) string {
	content := item.Content
	if item.ContentFormat == "html" {
		content = stripHTML(content)
	}
	parts := []string{
		strings.Repeat(item.Title+" ", 3),
		strings.Repeat(item.Category+" ", 2),
		strings.Repeat(strings.Join(item.Tags, " ")+" ", 2),
		item.Summary,
		content,
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
