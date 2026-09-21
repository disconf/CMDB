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
	mu         sync.RWMutex
	analyses   []Analysis
	executions []Execution
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{analyses: []Analysis{}, executions: []Execution{}}
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
