package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseExamples(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	files := []string{
		filepath.Join(root, "examples", "create-vm.yaml"),
		filepath.Join(root, "config.yaml"),
	}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			def, err := ParseFile(file)
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}
			if def.Version.String() != "1" {
				t.Fatalf("version = %q", def.Version)
			}
			if def.Name == "" {
				t.Fatal("name is empty")
			}
			if len(def.Steps) == 0 {
				t.Fatal("no steps")
			}
			for _, step := range def.Steps {
				if step.ID == "" {
					t.Fatal("step missing id")
				}
				if step.Request.Method == "" || step.Request.URL == "" {
					t.Fatalf("step %q missing request", step.ID)
				}
			}
		})
	}
}

func TestParseUnquotedVersion(t *testing.T) {
	t.Parallel()
	def, err := ParseBytes([]byte(`
version: 1
name: demo
steps:
  - id: ping
    request:
      method: GET
      url: https://example.test/ping
`))
	if err != nil {
		t.Fatal(err)
	}
	if def.Version.String() != "1" {
		t.Fatalf("version = %q", def.Version)
	}
}

func TestParseUnknownField(t *testing.T) {
	t.Parallel()
	_, err := ParseBytes([]byte(`
version: "1"
name: demo
unexpected: true
steps:
  - id: ping
    request:
      method: GET
      url: https://example.test/ping
`))
	if err == nil {
		t.Fatal("expected unknown field error")
	}
	if !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("error %q should mention unexpected", err)
	}
}

func TestParseCreateVMShape(t *testing.T) {
	t.Parallel()
	def, err := ParseFile(filepath.Join(moduleRoot(t), "examples", "create-vm.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if def.Name != "create-vm" {
		t.Fatalf("name = %q", def.Name)
	}
	if def.Services["cloudstack"] == "" || def.Services["dns"] == "" {
		t.Fatalf("services = %#v", def.Services)
	}
	if _, ok := def.Inputs["customer_id"]; !ok {
		t.Fatal("missing customer_id input")
	}
	if len(def.Steps) != 3 {
		t.Fatalf("len(steps) = %d", len(def.Steps))
	}
	vm := def.Steps[1]
	if vm.ID != "create_vm" {
		t.Fatalf("second step = %q", vm.ID)
	}
	if len(vm.DependsOn) != 1 || vm.DependsOn[0] != "create_network" {
		t.Fatalf("depends_on = %#v", vm.DependsOn)
	}
	if vm.Retry == nil || vm.Retry.Attempts != 3 || vm.Retry.Backoff != BackoffExponential {
		t.Fatalf("retry = %#v", vm.Retry)
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
