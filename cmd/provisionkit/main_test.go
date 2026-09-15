package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCmdValidateAndGraph(t *testing.T) {
	root := moduleRoot(t)
	file := filepath.Join(root, "examples", "create-vm.yaml")
	if err := cmdValidate([]string{file}); err != nil {
		t.Fatal(err)
	}
	if err := cmdGraph([]string{file}); err != nil {
		t.Fatal(err)
	}
}

func TestCmdValidateRejectsCycle(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "cycle.yaml")
	src := []byte(`
version: "1"
name: cycle
steps:
  - id: a
    depends_on: [b]
    request: {method: GET, url: https://example.test/a}
  - id: b
    depends_on: [a]
    request: {method: GET, url: https://example.test/b}
`)
	if err := os.WriteFile(file, src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdValidate([]string{file}); err == nil {
		t.Fatal("expected cycle error")
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
