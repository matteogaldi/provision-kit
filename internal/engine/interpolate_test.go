package engine

import (
	"strings"
	"testing"

	"github.com/matteogaldi/provision-kit/internal/store"
	"github.com/matteogaldi/provision-kit/internal/workflow"
)

func TestInterpolateMixedAndTyped(t *testing.T) {
	t.Parallel()
	rt := Runtime{
		Inputs:   map[string]string{"plan": "small"},
		Services: map[string]string{"cloud": "https://cloud.test"},
		Steps: map[string]*store.StepState{
			"net": {
				Status: store.StatusSucceeded,
				Output: map[string]any{"id": "net-1", "meta": map[string]any{"n": float64(3)}},
			},
		},
	}

	got, err := Interpolate("{{ services.cloud }}/vms", rt)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://cloud.test/vms" {
		t.Fatalf("got %v", got)
	}

	got, err = Interpolate("{{ steps.net.output.id }}", rt)
	if err != nil {
		t.Fatal(err)
	}
	if got != "net-1" {
		t.Fatalf("typed output = %v", got)
	}

	got, err = Interpolate(map[string]any{
		"plan": "{{ inputs.plan }}",
		"n":    "{{ steps.net.output.meta.n }}",
	}, rt)
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	if m["plan"] != "small" {
		t.Fatalf("plan = %v", m["plan"])
	}
	if m["n"] != float64(3) {
		t.Fatalf("n = %#v", m["n"])
	}
}

func TestInterpolateMissingField(t *testing.T) {
	t.Parallel()
	rt := Runtime{
		Steps: map[string]*store.StepState{
			"net": {Status: store.StatusSucceeded, Output: map[string]any{"id": "net-1"}},
		},
	}
	_, err := Interpolate("{{ steps.net.output.ip }}", rt)
	if err == nil || !strings.Contains(err.Error(), `no field "ip"`) {
		t.Fatalf("got %v", err)
	}
}

func TestParseRefUsedByInterpolate(t *testing.T) {
	t.Parallel()
	_, err := workflow.FindRefs("{{ steps.net.output }}")
	if err != nil {
		t.Fatal(err)
	}
}
