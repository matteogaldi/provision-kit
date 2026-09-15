package store

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Memory struct {
	mu  sync.Mutex
	ops map[string]*Operation
}

func NewMemory() *Memory {
	return &Memory{ops: map[string]*Operation{}}
}

func (m *Memory) Create(_ context.Context, op *Operation) error {
	if op == nil || op.ID == "" {
		return fmt.Errorf("operation id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.ops[op.ID]; exists {
		return fmt.Errorf("operation %s already exists", op.ID)
	}
	now := time.Now().UTC()
	stored := Clone(op)
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = now
	}
	stored.UpdatedAt = now
	m.ops[op.ID] = stored
	return nil
}

func (m *Memory) Get(_ context.Context, id string) (*Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	op, ok := m.ops[id]
	if !ok {
		return nil, ErrNotFound
	}
	return Clone(op), nil
}

func (m *Memory) Mutate(_ context.Context, id string, fn func(*Operation) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	op, ok := m.ops[id]
	if !ok {
		return ErrNotFound
	}
	if err := fn(op); err != nil {
		return err
	}
	op.UpdatedAt = time.Now().UTC()
	return nil
}
