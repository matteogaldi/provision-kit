package store

import (
	"encoding/json"
	"time"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

type Operation struct {
	ID           string                `json:"operation_id"`
	WorkflowName string                `json:"workflow"`
	Status       Status                `json:"status"`
	Inputs       map[string]string     `json:"inputs"`
	Steps        map[string]*StepState `json:"steps"`
	Error        string                `json:"error,omitempty"`
	CreatedAt    time.Time             `json:"created_at"`
	UpdatedAt    time.Time             `json:"updated_at"`
}

type StepState struct {
	ID          string     `json:"id"`
	Status      Status     `json:"status"`
	Attempt     int        `json:"attempt"`
	Output      any        `json:"output,omitempty"`
	HTTPStatus  int        `json:"http_status,omitempty"`
	Error       string     `json:"error,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

func Clone(op *Operation) *Operation {
	if op == nil {
		return nil
	}
	out := *op
	if op.Inputs != nil {
		out.Inputs = make(map[string]string, len(op.Inputs))
		for k, v := range op.Inputs {
			out.Inputs[k] = v
		}
	}
	if op.Steps != nil {
		out.Steps = make(map[string]*StepState, len(op.Steps))
		for k, v := range op.Steps {
			out.Steps[k] = CloneStep(v)
		}
	}
	return &out
}

func CloneStep(s *StepState) *StepState {
	if s == nil {
		return nil
	}
	out := *s
	out.Output = cloneJSON(s.Output)
	if s.StartedAt != nil {
		t := *s.StartedAt
		out.StartedAt = &t
	}
	if s.CompletedAt != nil {
		t := *s.CompletedAt
		out.CompletedAt = &t
	}
	return &out
}

func cloneJSON(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}
