package store

import (
	"context"
	"testing"
)

func TestMemoryRoundTrip(t *testing.T) {
	t.Parallel()
	s := NewMemory()
	op := &Operation{
		ID:           "op_1",
		WorkflowName: "demo",
		Status:       StatusPending,
		Inputs:       map[string]string{"a": "b"},
		Steps: map[string]*StepState{
			"ping": {ID: "ping", Status: StatusPending},
		},
	}
	if err := s.Create(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(context.Background(), "op_1")
	if err != nil {
		t.Fatal(err)
	}
	got.Inputs["a"] = "mutated"
	got.Steps["ping"].Status = StatusSucceeded

	again, err := s.Get(context.Background(), "op_1")
	if err != nil {
		t.Fatal(err)
	}
	if again.Inputs["a"] != "b" {
		t.Fatal("Get should return a clone")
	}
	if again.Steps["ping"].Status != StatusPending {
		t.Fatal("step clone failed")
	}

	err = s.Mutate(context.Background(), "op_1", func(o *Operation) error {
		o.Status = StatusRunning
		o.Steps["ping"].Status = StatusRunning
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err = s.Get(context.Background(), "op_1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusRunning || got.Steps["ping"].Status != StatusRunning {
		t.Fatalf("mutate did not persist: %#v", got)
	}
}
