package aiops

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"
)

type memoryRepository struct {
	mu            sync.RWMutex
	analyses      []Analysis
	knowledge     []Knowledge
	versions      []KnowledgeVersion
	attachments   []KnowledgeAttachment
	embeddings    map[string][]float32
	answers       []KnowledgeAnswer
	executions    []Execution
	runbooks      []RunbookPolicy
	changeControl ChangeControl
	canaries      []CanaryValidation
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{analyses: []Analysis{}, knowledge: []Knowledge{}, versions: []KnowledgeVersion{}, attachments: []KnowledgeAttachment{}, embeddings: map[string][]float32{}, answers: []KnowledgeAnswer{}, executions: []Execution{}, runbooks: []RunbookPolicy{}, changeControl: ChangeControl{ID: "default", LiveEnabled: true}, canaries: []CanaryValidation{}}
}

func (r *memoryRepository) LoadKnowledge(context.Context) ([]Knowledge, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Knowledge{}, r.knowledge...), nil
}

func (r *memoryRepository) SaveKnowledge(_ context.Context, item Knowledge) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.knowledge {
		if r.knowledge[i].ID == item.ID {
			r.knowledge[i] = item
			return nil
		}
	}
	r.knowledge = append([]Knowledge{item}, r.knowledge...)
	return nil
}

func (r *memoryRepository) DeleteKnowledge(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.knowledge {
		if r.knowledge[i].ID == id {
			r.knowledge = append(r.knowledge[:i], r.knowledge[i+1:]...)
			break
		}
	}
	filtered := r.versions[:0]
	for _, item := range r.versions {
		if item.KnowledgeID != id {
			filtered = append(filtered, item)
		}
	}
	r.versions = filtered
	attachments := r.attachments[:0]
	for _, item := range r.attachments {
		if item.KnowledgeID != id {
			attachments = append(attachments, item)
		}
	}
	r.attachments = attachments
	delete(r.embeddings, id)
	return nil
}

func (r *memoryRepository) SaveKnowledgeEmbedding(_ context.Context, knowledgeID, _ string, _ int, vector []float32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.embeddings[knowledgeID] = append([]float32(nil), vector...)
	return nil
}

func (r *memoryRepository) LoadKnowledgeEmbedding(_ context.Context, knowledgeID string) ([]float32, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	vector, ok := r.embeddings[knowledgeID]
	if !ok {
		return nil, nil
	}
	return append([]float32(nil), vector...), nil
}

func (r *memoryRepository) SearchKnowledgeVector(_ context.Context, vector []float32, limit int) ([]KnowledgeVectorMatch, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := []KnowledgeVectorMatch{}
	for knowledgeID, candidate := range r.embeddings {
		result = append(result, KnowledgeVectorMatch{KnowledgeID: knowledgeID, Score: cosineSimilarity(vector, candidate)})
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Score > result[j].Score })
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *memoryRepository) SaveKnowledgeAnswer(_ context.Context, item KnowledgeAnswer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.answers {
		if r.answers[i].ID == item.ID {
			r.answers[i] = item
			return nil
		}
	}
	r.answers = append([]KnowledgeAnswer{item}, r.answers...)
	return nil
}
func (r *memoryRepository) GetKnowledgeAnswer(_ context.Context, id string) (KnowledgeAnswer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, item := range r.answers {
		if item.ID == id {
			return item, nil
		}
	}
	return KnowledgeAnswer{}, ErrNotFound
}
func (r *memoryRepository) LoadKnowledgeAnswers(_ context.Context, limit int) ([]KnowledgeAnswer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if limit > 0 && len(r.answers) > limit {
		return append([]KnowledgeAnswer(nil), r.answers[:limit]...), nil
	}
	return append([]KnowledgeAnswer(nil), r.answers...), nil
}

func (r *memoryRepository) LoadKnowledgeAttachments(_ context.Context, knowledgeID string) ([]KnowledgeAttachment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := []KnowledgeAttachment{}
	for _, item := range r.attachments {
		if item.KnowledgeID == knowledgeID {
			result = append(result, item)
		}
	}
	return result, nil
}

func (r *memoryRepository) SaveKnowledgeAttachment(_ context.Context, item KnowledgeAttachment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.attachments {
		if r.attachments[i].ID == item.ID {
			r.attachments[i] = item
			return nil
		}
	}
	r.attachments = append([]KnowledgeAttachment{item}, r.attachments...)
	return nil
}

func (r *memoryRepository) DeleteKnowledgeAttachment(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.attachments {
		if r.attachments[i].ID == id {
			r.attachments = append(r.attachments[:i], r.attachments[i+1:]...)
			return nil
		}
	}
	return nil
}

func (r *memoryRepository) LoadKnowledgeVersions(_ context.Context, knowledgeID string) ([]KnowledgeVersion, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := []KnowledgeVersion{}
	for _, item := range r.versions {
		if item.KnowledgeID == knowledgeID {
			result = append(result, item)
		}
	}
	return result, nil
}

func (r *memoryRepository) SaveKnowledgeVersion(_ context.Context, item KnowledgeVersion) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.versions {
		if r.versions[i].ID == item.ID {
			r.versions[i] = item
			return nil
		}
	}
	r.versions = append([]KnowledgeVersion{item}, r.versions...)
	return nil
}

func (r *memoryRepository) LoadAnalyses(context.Context) ([]Analysis, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Analysis{}, r.analyses...), nil
}

func (r *memoryRepository) SaveAnalysis(_ context.Context, item Analysis) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.analyses {
		if r.analyses[i].ID == item.ID {
			r.analyses[i] = item
			return nil
		}
	}
	r.analyses = append([]Analysis{item}, r.analyses...)
	return nil
}

func (r *memoryRepository) LoadExecutions(context.Context) ([]Execution, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Execution{}, r.executions...), nil
}

func (r *memoryRepository) SaveExecution(_ context.Context, item Execution) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.executions {
		if r.executions[i].ID == item.ID {
			r.executions[i] = item
			return nil
		}
	}
	r.executions = append([]Execution{item}, r.executions...)
	return nil
}

func (r *memoryRepository) LoadRunbookPolicies(context.Context) ([]RunbookPolicy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]RunbookPolicy{}, r.runbooks...), nil
}

func (r *memoryRepository) SaveRunbookPolicy(_ context.Context, item RunbookPolicy) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.runbooks {
		if r.runbooks[i].RunbookID == item.RunbookID {
			r.runbooks[i] = item
			return nil
		}
	}
	r.runbooks = append(r.runbooks, item)
	return nil
}

func (r *memoryRepository) LoadChangeControl(context.Context) (ChangeControl, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.changeControl, nil
}

func (r *memoryRepository) SaveChangeControl(_ context.Context, item ChangeControl) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.changeControl = item
	return nil
}

func (r *memoryRepository) LoadCanaryValidations(context.Context) ([]CanaryValidation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]CanaryValidation{}, r.canaries...), nil
}

func (r *memoryRepository) SaveCanaryValidation(_ context.Context, item CanaryValidation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.canaries {
		if r.canaries[i].ID == item.ID {
			r.canaries[i] = item
			return nil
		}
	}
	r.canaries = append([]CanaryValidation{item}, r.canaries...)
	return nil
}
func newID(prefix string) string {
	var value [8]byte
	if _, err := rand.Read(value[:]); err == nil {
		return prefix + "-" + hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
