package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/matteogaldi/provision-kit/internal/engine"
	"github.com/matteogaldi/provision-kit/internal/store"
	"github.com/matteogaldi/provision-kit/internal/workflow"
)

func TestProvisionAcceptedAndInspect(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "acc-1"})
	}))
	t.Cleanup(upstream.Close)

	def, err := workflow.ParseBytes([]byte(fmt.Sprintf(`
version: "1"
name: create-account
services:
  cloud: %q
inputs:
  customer_id: {type: string}
steps:
  - id: create-account
    request:
      method: POST
      url: "{{ services.cloud }}/accounts"
      body:
        customerId: "{{ inputs.customer_id }}"
`, upstream.URL)))
	if err != nil {
		t.Fatal(err)
	}
	if err := workflow.Validate(def); err != nil {
		t.Fatal(err)
	}

	st := store.NewMemory()
	eng := engine.New(engine.Options{Store: st, Sleep: func(time.Duration) {}})
	srv := httptest.NewServer(New(map[string]*workflow.Definition{def.Name: def}, st, eng).Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/v1/provision/create-account", "application/json", bytes.NewBufferString(`{"inputs":{"customer_id":"acme"}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var start struct {
		OperationID string `json:"operation_id"`
		Status      string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&start); err != nil {
		t.Fatal(err)
	}
	if start.OperationID == "" || start.Status != string(store.StatusPending) {
		t.Fatalf("start = %#v", start)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		got := getOp(t, srv.URL, start.OperationID)
		if got.Status == store.StatusSucceeded {
			if got.Steps["create-account"].Output.(map[string]any)["id"] != "acc-1" {
				t.Fatalf("output = %#v", got.Steps["create-account"].Output)
			}
			return
		}
		if got.Status == store.StatusFailed {
			t.Fatalf("failed: %s", got.Error)
		}
		if time.Now().After(deadline) {
			t.Fatal("timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestUnknownWorkflow(t *testing.T) {
	st := store.NewMemory()
	eng := engine.New(engine.Options{Store: st})
	srv := httptest.NewServer(New(nil, st, eng).Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/v1/provision/nope", "application/json", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestMissingInput(t *testing.T) {
	def, err := workflow.ParseBytes([]byte(`
version: "1"
name: demo
inputs:
  n: {type: string}
steps:
  - id: ping
    request: {method: GET, url: https://example.test/x}
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := workflow.Validate(def); err != nil {
		t.Fatal(err)
	}
	st := store.NewMemory()
	eng := engine.New(engine.Options{Store: st})
	srv := httptest.NewServer(New(map[string]*workflow.Definition{def.Name: def}, st, eng).Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/v1/provision/demo", "application/json", bytes.NewBufferString(`{"inputs":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func getOp(t *testing.T, base, id string) *store.Operation {
	t.Helper()
	resp, err := http.Get(base + "/v1/operations/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("inspect status = %d", resp.StatusCode)
	}
	var op store.Operation
	if err := json.NewDecoder(resp.Body).Decode(&op); err != nil {
		t.Fatal(err)
	}
	return &op
}
