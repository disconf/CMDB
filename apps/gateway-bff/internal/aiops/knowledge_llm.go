package aiops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type knowledgeLLMProvider interface {
	Name() string
	Generate(context.Context, string) (string, error)
}

type externalKnowledgeLLMProvider struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func newKnowledgeLLMProvider() knowledgeLLMProvider {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("AI_LLM_BASE_URL")), "/")
	model := strings.TrimSpace(os.Getenv("AI_LLM_MODEL"))
	if baseURL == "" || model == "" {
		return nil
	}
	return &externalKnowledgeLLMProvider{baseURL: baseURL, apiKey: strings.TrimSpace(os.Getenv("AI_LLM_API_KEY")), model: model, client: &http.Client{Timeout: 30 * time.Second}}
}

func (p *externalKnowledgeLLMProvider) Name() string { return p.model }
func (p *externalKnowledgeLLMProvider) Generate(ctx context.Context, prompt string) (string, error) {
	payload, err := json.Marshal(map[string]any{"model": p.model, "temperature": 0.1, "messages": []map[string]string{{"role": "system", "content": "你是企业运维助手。只能依据给定知识上下文回答，必须保留引用编号，不得编造。若上下文不足，明确说明。"}, {"role": "user", "content": prompt}}})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	response, err := p.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var document struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err = json.NewDecoder(response.Body).Decode(&document); err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || len(document.Choices) == 0 {
		return "", fmt.Errorf("LLM provider returned %s", response.Status)
	}
	return strings.TrimSpace(document.Choices[0].Message.Content), nil
}
