package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type SQLite struct {
	db *sql.DB
}

func OpenSQLite(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS operations (
	id TEXT PRIMARY KEY,
	payload TEXT NOT NULL
)`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLite{db: db}, nil
}

func (s *SQLite) Close() error {
	return s.db.Close()
}

func (s *SQLite) Create(ctx context.Context, op *Operation) error {
	if op == nil || op.ID == "" {
		return fmt.Errorf("operation id is required")
	}
	stored := Clone(op)
	now := time.Now().UTC()
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = now
	}
	stored.UpdatedAt = now
	payload, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO operations (id, payload) VALUES (?, ?)`, stored.ID, string(payload))
	if err != nil {
		return err
	}
	return nil
}

func (s *SQLite) Get(ctx context.Context, id string) (*Operation, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM operations WHERE id = ?`, id).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return decodeOperation(payload)
}

func (s *SQLite) Mutate(ctx context.Context, id string, fn func(*Operation) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var payload string
	err = tx.QueryRowContext(ctx, `SELECT payload FROM operations WHERE id = ?`, id).Scan(&payload)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	op, err := decodeOperation(payload)
	if err != nil {
		return err
	}
	if err := fn(op); err != nil {
		return err
	}
	op.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(op)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE operations SET payload = ? WHERE id = ?`, string(raw), id); err != nil {
		return err
	}
	return tx.Commit()
}

func decodeOperation(payload string) (*Operation, error) {
	var op Operation
	if err := json.Unmarshal([]byte(payload), &op); err != nil {
		return nil, err
	}
	if op.Inputs == nil {
		op.Inputs = map[string]string{}
	}
	if op.Steps == nil {
		op.Steps = map[string]*StepState{}
	}
	return &op, nil
}
