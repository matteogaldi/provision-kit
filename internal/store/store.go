package store

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("operation not found")

type Store interface {
	Create(ctx context.Context, op *Operation) error
	Get(ctx context.Context, id string) (*Operation, error)
	Mutate(ctx context.Context, id string, fn func(*Operation) error) error
}
