package workflow

import (
	"strings"
	"testing"
)

func TestBuildGraphLinear(t *testing.T) {
	t.Parallel()
	g, err := BuildGraph([]StepDef{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(g.Order, ",")
	if got != "a,b,c" {
		t.Fatalf("order = %s", got)
	}
	ready := g.Ready(map[string]bool{})
	if strings.Join(ready, ",") != "a" {
		t.Fatalf("ready = %v", ready)
	}
	ready = g.Ready(map[string]bool{"a": true})
	if strings.Join(ready, ",") != "b" {
		t.Fatalf("ready after a = %v", ready)
	}
}

func TestBuildGraphFanIn(t *testing.T) {
	t.Parallel()
	g, err := BuildGraph([]StepDef{
		{ID: "a"},
		{ID: "b"},
		{ID: "c", DependsOn: []string{"a", "b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(g.Order, ",") != "a,b,c" {
		t.Fatalf("order = %v", g.Order)
	}
	ready := g.Ready(map[string]bool{})
	if strings.Join(ready, ",") != "a,b" {
		t.Fatalf("ready = %v", ready)
	}
	if !g.DependsOnTransitively("c", "a") || !g.DependsOnTransitively("c", "b") {
		t.Fatal("c should depend on a and b")
	}
	if g.DependsOnTransitively("a", "b") {
		t.Fatal("a should not depend on b")
	}
}

func TestBuildGraphCycle(t *testing.T) {
	t.Parallel()
	_, err := BuildGraph([]StepDef{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"a"}},
	})
	if err == nil || !strings.Contains(err.Error(), "circular") {
		t.Fatalf("got %v", err)
	}
}
