package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/matteogaldi/provision-kit/internal/store"
	"github.com/matteogaldi/provision-kit/internal/workflow"
)

const defaultMaxParallel = 8

type Options struct {
	Store       store.Store
	Client      *http.Client
	Sleep       func(time.Duration)
	MaxParallel int
	Now         func() time.Time
}

type Engine struct {
	store       store.Store
	client      *http.Client
	sleep       func(time.Duration)
	maxParallel int
	now         func() time.Time
}

func New(opts Options) *Engine {
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: 30 * time.Second}
	}
	if opts.Sleep == nil {
		opts.Sleep = time.Sleep
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	if opts.MaxParallel < 1 {
		opts.MaxParallel = defaultMaxParallel
	}
	return &Engine{
		store:       opts.Store,
		client:      opts.Client,
		sleep:       opts.Sleep,
		maxParallel: opts.MaxParallel,
		now:         opts.Now,
	}
}

func (e *Engine) Create(ctx context.Context, def *workflow.Definition, inputs map[string]string) (*store.Operation, error) {
	if inputs == nil {
		inputs = map[string]string{}
	}
	if err := workflow.CheckInputs(def, inputs); err != nil {
		return nil, err
	}
	if def.Graph == nil {
		if err := workflow.Validate(def); err != nil {
			return nil, err
		}
	}

	id, err := newOperationID()
	if err != nil {
		return nil, err
	}
	steps := make(map[string]*store.StepState, len(def.Steps))
	for _, step := range def.Steps {
		steps[step.ID] = &store.StepState{ID: step.ID, Status: store.StatusPending}
	}
	op := &store.Operation{
		ID:           id,
		WorkflowName: def.Name,
		Status:       store.StatusPending,
		Inputs:       inputs,
		Steps:        steps,
		CreatedAt:    e.now(),
		UpdatedAt:    e.now(),
	}
	if err := e.store.Create(ctx, op); err != nil {
		return nil, err
	}
	return store.Clone(op), nil
}

func (e *Engine) Start(ctx context.Context, def *workflow.Definition, inputs map[string]string) (*store.Operation, error) {
	op, err := e.Create(ctx, def, inputs)
	if err != nil {
		return nil, err
	}
	go func() {
		_ = e.Execute(context.Background(), def, op.ID)
	}()
	return op, nil
}

func (e *Engine) Run(ctx context.Context, def *workflow.Definition, inputs map[string]string) (*store.Operation, error) {
	op, err := e.Create(ctx, def, inputs)
	if err != nil {
		return nil, err
	}
	if err := e.Execute(ctx, def, op.ID); err != nil {
		got, getErr := e.store.Get(ctx, op.ID)
		if getErr != nil {
			return nil, err
		}
		return got, err
	}
	return e.store.Get(ctx, op.ID)
}

func (e *Engine) Execute(ctx context.Context, def *workflow.Definition, opID string) error {
	err := e.store.Mutate(ctx, opID, func(op *store.Operation) error {
		op.Status = store.StatusRunning
		return nil
	})
	if err != nil {
		return err
	}

	for {
		if err := ctx.Err(); err != nil {
			_ = e.failOperation(ctx, opID, err.Error())
			return err
		}

		op, err := e.store.Get(ctx, opID)
		if err != nil {
			return err
		}

		completed := map[string]bool{}
		failed := false
		pending := 0
		for id, st := range op.Steps {
			switch st.Status {
			case store.StatusSucceeded:
				completed[id] = true
			case store.StatusFailed:
				failed = true
			case store.StatusPending, store.StatusRunning:
				pending++
			}
		}
		if failed {
			return e.failOperation(ctx, opID, firstStepError(op))
		}
		if pending == 0 {
			return e.store.Mutate(ctx, opID, func(o *store.Operation) error {
				o.Status = store.StatusSucceeded
				o.Error = ""
				return nil
			})
		}

		var ready []string
		for _, id := range def.Graph.Ready(completed) {
			if op.Steps[id] != nil && op.Steps[id].Status == store.StatusPending {
				ready = append(ready, id)
			}
		}
		if len(ready) == 0 {
			return e.failOperation(ctx, opID, "no runnable steps remain")
		}
		if len(ready) > e.maxParallel {
			ready = ready[:e.maxParallel]
		}

		var wg sync.WaitGroup
		errCh := make(chan error, len(ready))
		for _, stepID := range ready {
			stepDef, ok := def.Step(stepID)
			if !ok {
				return e.failOperation(ctx, opID, fmt.Sprintf("unknown step %q", stepID))
			}
			wg.Add(1)
			go func(stepDef workflow.StepDef) {
				defer wg.Done()
				if err := e.runStep(ctx, def, opID, stepDef); err != nil {
					errCh <- err
				}
			}(stepDef)
		}
		wg.Wait()
		close(errCh)
		if err, ok := <-errCh; ok {
			msg := err.Error()
			_ = e.failOperation(ctx, opID, msg)
			return err
		}
	}
}

func (e *Engine) runStep(ctx context.Context, def *workflow.Definition, opID string, step workflow.StepDef) error {
	retry := step.RetryOrDefault()
	var lastErr error
	for attempt := 1; attempt <= retry.Attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		start := e.now()
		_ = e.store.Mutate(ctx, opID, func(op *store.Operation) error {
			st := op.Steps[step.ID]
			st.Status = store.StatusRunning
			st.Attempt = attempt
			st.Error = ""
			st.StartedAt = &start
			st.CompletedAt = nil
			return nil
		})

		status, output, err := e.attempt(ctx, def, opID, step)
		if err == nil {
			done := e.now()
			return e.store.Mutate(ctx, opID, func(op *store.Operation) error {
				st := op.Steps[step.ID]
				st.Status = store.StatusSucceeded
				st.Output = output
				st.HTTPStatus = status
				st.Error = ""
				st.CompletedAt = &done
				return nil
			})
		}
		lastErr = err
		done := e.now()
		_ = e.store.Mutate(ctx, opID, func(op *store.Operation) error {
			st := op.Steps[step.ID]
			st.HTTPStatus = status
			st.Error = err.Error()
			st.CompletedAt = &done
			return nil
		})
		if attempt < retry.Attempts {
			e.sleep(backoffDelay(retry, attempt))
		}
	}

	done := e.now()
	_ = e.store.Mutate(ctx, opID, func(op *store.Operation) error {
		st := op.Steps[step.ID]
		st.Status = store.StatusFailed
		st.CompletedAt = &done
		if lastErr != nil {
			st.Error = lastErr.Error()
		}
		return nil
	})
	if lastErr == nil {
		lastErr = fmt.Errorf("step %q failed", step.ID)
	}
	return fmt.Errorf("step %q: %w", step.ID, lastErr)
}

func (e *Engine) attempt(ctx context.Context, def *workflow.Definition, opID string, step workflow.StepDef) (int, any, error) {
	op, err := e.store.Get(ctx, opID)
	if err != nil {
		return 0, nil, err
	}
	rt := Runtime{
		Inputs:   op.Inputs,
		Services: def.Services,
		Steps:    op.Steps,
	}

	urlVal, err := Interpolate(step.Request.URL, rt)
	if err != nil {
		return 0, nil, fmt.Errorf("interpolating request.url: %w", err)
	}
	url, ok := urlVal.(string)
	if !ok || strings.TrimSpace(url) == "" {
		return 0, nil, fmt.Errorf("request.url interpolated to a non-string value")
	}

	headers := map[string]string{}
	for k, v := range step.Request.Headers {
		hv, err := Interpolate(v, rt)
		if err != nil {
			return 0, nil, fmt.Errorf("interpolating request.headers.%s: %w", k, err)
		}
		headers[k] = stringify(hv)
	}

	var body any
	if step.Request.Body != nil {
		body, err = Interpolate(step.Request.Body, rt)
		if err != nil {
			return 0, nil, fmt.Errorf("interpolating request.body: %w", err)
		}
	}

	return e.doRequest(ctx, step.Request.Method, url, headers, body)
}

func (e *Engine) doRequest(ctx context.Context, method, url string, headers map[string]string, body any) (int, any, error) {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("encode request body: %w", err)
		}
		rdr = bytes.NewReader(raw)
		if _, ok := headerValue(headers, "Content-Type"); !ok {
			headers["Content-Type"] = "application/json"
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return 0, nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 256 {
			msg = msg[:256]
		}
		if msg == "" {
			msg = resp.Status
		}
		return resp.StatusCode, nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return resp.StatusCode, nil, nil
	}
	var output any
	if err := json.Unmarshal(trimmed, &output); err != nil {
		return resp.StatusCode, nil, nil
	}
	return resp.StatusCode, output, nil
}

func (e *Engine) failOperation(ctx context.Context, opID, msg string) error {
	_ = e.store.Mutate(ctx, opID, func(op *store.Operation) error {
		op.Status = store.StatusFailed
		op.Error = msg
		return nil
	})
	return fmt.Errorf("%s", msg)
}

func firstStepError(op *store.Operation) string {
	for _, st := range op.Steps {
		if st.Status == store.StatusFailed && st.Error != "" {
			return st.Error
		}
	}
	return "operation failed"
}

func headerValue(headers map[string]string, key string) (string, bool) {
	for k, v := range headers {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return "", false
}

func newOperationID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "op_" + hex.EncodeToString(b[:]), nil
}
