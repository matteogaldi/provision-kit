package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matteogaldi/provision-kit/internal/store"
	"github.com/matteogaldi/provision-kit/internal/workflow"
)

func TestRunLinearWorkflow(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/networks", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if body["customerId"] != "acme" {
			t.Errorf("customerId = %v", body["customerId"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "net-1"})
	})
	mux.HandleFunc("/vms", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if body["networkId"] != "net-1" || body["plan"] != "small" {
			t.Errorf("body = %#v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "vm-1", "ip": "10.0.0.8"})
	})
	mux.HandleFunc("/records", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["ip"] != "10.0.0.8" {
			t.Errorf("ip = %v", body["ip"])
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	def := mustDef(t, fmt.Sprintf(`
version: "1"
name: create-vm
services:
  cloudstack: %q
  dns: %q
inputs:
  customer_id: {type: string}
  plan: {type: string}
steps:
  - id: create_network
    request:
      method: POST
      url: "{{ services.cloudstack }}/networks"
      body:
        customerId: "{{ inputs.customer_id }}"
  - id: create_vm
    depends_on: [create_network]
    request:
      method: POST
      url: "{{ services.cloudstack }}/vms"
      body:
        networkId: "{{ steps.create_network.output.id }}"
        plan: "{{ inputs.plan }}"
  - id: configure_dns
    depends_on: [create_vm]
    request:
      method: POST
      url: "{{ services.dns }}/records"
      body:
        ip: "{{ steps.create_vm.output.ip }}"
`, srv.URL, srv.URL))

	eng := New(Options{Store: store.NewMemory(), Sleep: func(time.Duration) {}})
	op, err := eng.Run(context.Background(), def, map[string]string{
		"customer_id": "acme",
		"plan":        "small",
	})
	if err != nil {
		t.Fatal(err)
	}
	if op.Status != store.StatusSucceeded {
		t.Fatalf("status = %s error=%s", op.Status, op.Error)
	}
	if op.Steps["create_network"].Output.(map[string]any)["id"] != "net-1" {
		t.Fatalf("network output = %#v", op.Steps["create_network"].Output)
	}
	if op.Steps["configure_dns"].HTTPStatus != http.StatusCreated {
		t.Fatalf("dns status = %d", op.Steps["configure_dns"].HTTPStatus)
	}
}

func TestRunRetries(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	t.Cleanup(srv.Close)

	def := mustDef(t, fmt.Sprintf(`
version: "1"
name: retry-demo
services:
  api: %q
steps:
  - id: ping
    request:
      method: POST
      url: "{{ services.api }}/x"
    retry:
      attempts: 3
      backoff: exponential
      delay: 1ms
`, srv.URL))

	eng := New(Options{Store: store.NewMemory(), Sleep: func(time.Duration) {}})
	op, err := eng.Run(context.Background(), def, nil)
	if err != nil {
		t.Fatal(err)
	}
	if op.Status != store.StatusSucceeded {
		t.Fatalf("status = %s", op.Status)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d", calls.Load())
	}
	if op.Steps["ping"].Attempt != 3 {
		t.Fatalf("attempt = %d", op.Steps["ping"].Attempt)
	}
}

func TestRunStopsOnFailure(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/ok") {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "1"})
			return
		}
		http.Error(w, "nope", http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)

	def := mustDef(t, fmt.Sprintf(`
version: "1"
name: fail-demo
services:
  api: %q
steps:
  - id: ok
    request:
      method: POST
      url: "{{ services.api }}/ok"
  - id: bad
    depends_on: [ok]
    request:
      method: POST
      url: "{{ services.api }}/bad"
  - id: later
    depends_on: [bad]
    request:
      method: POST
      url: "{{ services.api }}/later"
`, srv.URL))

	eng := New(Options{Store: store.NewMemory(), Sleep: func(time.Duration) {}})
	op, err := eng.Run(context.Background(), def, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if op.Status != store.StatusFailed {
		t.Fatalf("status = %s", op.Status)
	}
	if op.Steps["ok"].Status != store.StatusSucceeded {
		t.Fatalf("ok = %s", op.Steps["ok"].Status)
	}
	if op.Steps["bad"].Status != store.StatusFailed {
		t.Fatalf("bad = %s", op.Steps["bad"].Status)
	}
	if op.Steps["later"].Status != store.StatusPending {
		t.Fatalf("later = %s", op.Steps["later"].Status)
	}
}

func TestRunConcurrentReadySteps(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	started := 0
	proceed := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		started++
		n := started
		mu.Unlock()
		if n == 2 {
			close(proceed)
		}
		select {
		case <-proceed:
		case <-time.After(2 * time.Second):
			t.Errorf("timeout waiting for concurrent sibling")
			http.Error(w, "timeout", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": r.URL.Path})
	}))
	t.Cleanup(srv.Close)

	def := mustDef(t, fmt.Sprintf(`
version: "1"
name: fanout
services:
  api: %q
steps:
  - id: a
    request: {method: POST, url: "{{ services.api }}/a"}
  - id: b
    request: {method: POST, url: "{{ services.api }}/b"}
  - id: join
    depends_on: [a, b]
    request:
      method: POST
      url: "{{ services.api }}/join"
      body:
        a: "{{ steps.a.output.ok }}"
        b: "{{ steps.b.output.ok }}"
`, srv.URL))

	eng := New(Options{Store: store.NewMemory(), MaxParallel: 8, Sleep: func(time.Duration) {}})
	op, err := eng.Run(context.Background(), def, nil)
	if err != nil {
		t.Fatal(err)
	}
	if op.Status != store.StatusSucceeded {
		t.Fatalf("status = %s error=%s", op.Status, op.Error)
	}
}

func TestCreateRejectsUnknownInputs(t *testing.T) {
	t.Parallel()
	def := mustDef(t, `
version: "1"
name: demo
inputs:
  n: {type: string}
steps:
  - id: ping
    request: {method: GET, url: https://example.test/x}
`)
	eng := New(Options{Store: store.NewMemory()})
	_, err := eng.Create(context.Background(), def, map[string]string{"n": "1", "extra": "2"})
	if err == nil || !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("got %v", err)
	}
}

func TestStartIsAsync(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		_, _ = io.Copy(io.Discard, r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	t.Cleanup(srv.Close)

	def := mustDef(t, fmt.Sprintf(`
version: "1"
name: async
services:
  api: %q
steps:
  - id: ping
    request: {method: POST, url: "{{ services.api }}/x"}
`, srv.URL))

	st := store.NewMemory()
	eng := New(Options{Store: st, Sleep: func(time.Duration) {}})
	op, err := eng.Start(context.Background(), def, nil)
	if err != nil {
		t.Fatal(err)
	}
	if op.Status != store.StatusPending {
		t.Fatalf("start status = %s", op.Status)
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := st.Get(context.Background(), op.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == store.StatusSucceeded {
			return
		}
		if got.Status == store.StatusFailed {
			t.Fatalf("failed: %s", got.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("operation did not complete")
}

func TestBackoffDelay(t *testing.T) {
	t.Parallel()
	spec := workflow.RetrySpec{Backoff: workflow.BackoffExponential, Delay: workflow.Duration(10 * time.Millisecond)}
	if d := backoffDelay(spec, 1); d != 10*time.Millisecond {
		t.Fatalf("attempt 1 = %s", d)
	}
	if d := backoffDelay(spec, 2); d != 20*time.Millisecond {
		t.Fatalf("attempt 2 = %s", d)
	}
	spec.Backoff = workflow.BackoffNone
	if d := backoffDelay(spec, 3); d != 0 {
		t.Fatalf("none = %s", d)
	}
}

func TestRunWithSQLite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "ok"})
	}))
	t.Cleanup(srv.Close)

	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "ops.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	def := mustDef(t, fmt.Sprintf(`
version: "1"
name: sqlite-demo
services:
  api: %q
steps:
  - id: a
    request: {method: POST, url: "{{ services.api }}/a"}
  - id: b
    request: {method: POST, url: "{{ services.api }}/b"}
  - id: join
    depends_on: [a, b]
    request: {method: POST, url: "{{ services.api }}/join"}
`, srv.URL))

	eng := New(Options{Store: st, MaxParallel: 8, Sleep: func(time.Duration) {}})
	op, err := eng.Run(context.Background(), def, nil)
	if err != nil {
		t.Fatal(err)
	}
	if op.Status != store.StatusSucceeded {
		t.Fatalf("status = %s error=%s", op.Status, op.Error)
	}
	got, err := st.Get(context.Background(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusSucceeded {
		t.Fatalf("sqlite get status = %s", got.Status)
	}
}

func mustDef(t *testing.T, src string) *workflow.Definition {
	t.Helper()
	def, err := workflow.ParseBytes([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := workflow.Validate(def); err != nil {
		t.Fatal(err)
	}
	return def
}
