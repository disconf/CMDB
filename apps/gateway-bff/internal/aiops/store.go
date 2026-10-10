package aiops

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

type memoryRepository struct {
	mu            sync.RWMutex
	analyses      []Analysis
	knowledge     []Knowledge
	executions    []Execution
	runbooks      []RunbookPolicy
	changeControl ChangeControl
	canaries      []CanaryValidation
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{analyses: []Analysis{}, knowledge: []Knowledge{}, executions: []Execution{}, runbooks: []RunbookPolicy{}, changeControl: ChangeControl{ID: "default", LiveEnabled: true}, canaries: []CanaryValidation{}}
}

func (r *memoryRepository) LoadKnowledge(context.Context) ([]Knowledge, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Knowledge(nil), r.knowledge...), nil
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
			return nil
		}
	}
	return nil
}

func (r *memoryRepository) LoadAnalyses(context.Context) ([]Analysis, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Analysis(nil), r.analyses...), nil
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
	return append([]Execution(nil), r.executions...), nil
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
	return append([]RunbookPolicy(nil), r.runbooks...), nil
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
	return append([]CanaryValidation(nil), r.canaries...), nil
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
