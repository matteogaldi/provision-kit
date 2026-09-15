package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAllDirectory(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	defs, err := LoadAll(filepath.Join(root, "examples"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := defs["create-vm"]; !ok {
		t.Fatalf("got names %v", keys(defs))
	}
}

func TestLoadAllDuplicateName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := []byte(`
version: "1"
name: demo
steps:
  - id: ping
    request: {method: GET, url: https://example.test/x}
`)
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.yaml"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAll(dir); err == nil {
		t.Fatal("expected duplicate name error")
	}
}

func keys(m map[string]*Definition) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
