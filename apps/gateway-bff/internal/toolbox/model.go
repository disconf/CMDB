package toolbox

import (
	"context"
	"errors"
)

var (
	ErrValidation    = errors.New("validation")
	ErrUnavailable   = errors.New("tool unavailable")
	ErrNotConfigured = errors.New("tool not configured")
)

type InputField struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder"`
	Required    bool   `json:"required"`
	Secret      bool   `json:"secret,omitempty"`
}

type Tool struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Category    string       `json:"category"`
	Description string       `json:"description"`
	Icon        string       `json:"icon"`
	Placeholder string       `json:"placeholder"`
	Risk        string       `json:"risk"`
	Configured  bool         `json:"configured"`
	ReadOnly    bool         `json:"readOnly"`
	Fields      []InputField `json:"fields"`
}

type Line struct {
	Time  string `json:"time"`
	Level string `json:"level"`
	Text  string `json:"text"`
}

type Result struct {
	ID         string            `json:"id"`
	ToolID     string            `json:"toolId"`
	ToolName   string            `json:"toolName"`
	Category   string            `json:"category"`
	Target     string            `json:"target"`
	Status     string            `json:"status"`
	Operator   string            `json:"operator"`
	StartedAt  string            `json:"startedAt"`
	FinishedAt string            `json:"finishedAt"`
	Duration   string            `json:"duration"`
	DurationMs int64             `json:"durationMs"`
	Summary    string            `json:"summary"`
	Error      string            `json:"error,omitempty"`
	Output     []Line            `json:"output"`
	Parameters map[string]string `json:"parameters,omitempty"`
}

type RunInput struct {
	ToolID       string `json:"toolId"`
	Target       string `json:"target"`
	Query        string `json:"query"`
	CredentialID string `json:"credentialId"`
	Namespace    string `json:"namespace"`
	Name         string `json:"name"`
	Container    string `json:"container"`
	Environment  string `json:"environment"`
	Operator     string `json:"operator"`
}

type Summary struct {
	Tools           int     `json:"tools"`
	ConfiguredTools int     `json:"configuredTools"`
	ExecutionsToday int     `json:"executionsToday"`
	SuccessRate     float64 `json:"successRate"`
	AvgDuration     string  `json:"avgDuration"`
	FailedToday     int     `json:"failedToday"`
}

type executionStore interface {
	Save(context.Context, Result) error
	List(context.Context, int) ([]Result, error)
	Summary(context.Context) (Summary, error)
}
