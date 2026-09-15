package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSQLiteRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.db")
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	op := &Operation{
		ID:           "op_sql",
		WorkflowName: "demo",
		Status:       StatusPending,
		Inputs:       map[string]string{"a": "b"},
		Steps: map[string]*StepState{
			"ping": {ID: "ping", Status: StatusPending, Output: map[string]any{"n": 1}},
		},
	}
	if err := s.Create(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(context.Background(), "op_sql")
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkflowName != "demo" || got.Inputs["a"] != "b" {
		t.Fatalf("got %#v", got)
	}

	err = s.Mutate(context.Background(), "op_sql", func(o *Operation) error {
		o.Status = StatusSucceeded
		o.Steps["ping"].Status = StatusSucceeded
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err = s.Get(context.Background(), "op_sql")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusSucceeded || got.Steps["ping"].Status != StatusSucceeded {
		t.Fatalf("mutate: %#v", got)
	}

	_, err = s.Get(context.Background(), "missing")
	if err != ErrNotFound {
		t.Fatalf("missing: %v", err)
	}
}
