package workflow

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadExamples(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	for _, file := range []string{
		filepath.Join(root, "examples", "create-vm.yaml"),
		filepath.Join(root, "config.yaml"),
	} {
		def, err := Load(file)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if def.Graph == nil || len(def.Graph.Order) != len(def.Steps) {
			t.Fatalf("%s: graph not built", file)
		}
	}
}

func TestValidateErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		yaml    string
		want    []string
		wantErr bool
	}{
		{
			name: "valid minimal",
			yaml: `
version: "1"
name: demo
steps:
  - id: ping
    request:
      method: GET
      url: https://example.test/ping
`,
		},
		{
			name: "duplicate ids",
			yaml: `
version: "1"
name: demo
steps:
  - id: ping
    request: {method: GET, url: https://example.test/a}
  - id: ping
    request: {method: GET, url: https://example.test/b}
`,
			wantErr: true,
			want:    []string{`duplicate step id`},
		},
		{
			name: "missing dependency",
			yaml: `
version: "1"
name: demo
steps:
  - id: ping
    depends_on: [missing]
    request: {method: GET, url: https://example.test/a}
`,
			wantErr: true,
			want:    []string{`unknown step "missing"`},
		},
		{
			name: "cycle",
			yaml: `
version: "1"
name: demo
steps:
  - id: a
    depends_on: [b]
    request: {method: GET, url: https://example.test/a}
  - id: b
    depends_on: [a]
    request: {method: GET, url: https://example.test/b}
`,
			wantErr: true,
			want:    []string{`circular dependency`},
		},
		{
			name: "unknown input ref",
			yaml: `
version: "1"
name: demo
steps:
  - id: ping
    request:
      method: GET
      url: "https://example.test/{{ inputs.nope }}"
`,
			wantErr: true,
			want:    []string{`unknown input "nope"`},
		},
		{
			name: "unknown service ref",
			yaml: `
version: "1"
name: demo
steps:
  - id: ping
    request:
      method: GET
      url: "{{ services.cloud }}/x"
`,
			wantErr: true,
			want:    []string{`unknown service "cloud"`},
		},
		{
			name: "ref to non-dependency",
			yaml: `
version: "1"
name: demo
steps:
  - id: a
    request: {method: GET, url: https://example.test/a}
  - id: b
    request:
      method: POST
      url: https://example.test/b
      body:
        x: "{{ steps.a.output.id }}"
`,
			wantErr: true,
			want:    []string{`not a dependency`},
		},
		{
			name: "bad retry",
			yaml: `
version: "1"
name: demo
steps:
  - id: ping
    request: {method: GET, url: https://example.test/a}
    retry:
      attempts: 0
      backoff: linear
`,
			wantErr: true,
			want:    []string{"must be >= 1", "unsupported backoff"},
		},
		{
			name: "bad method",
			yaml: `
version: "1"
name: demo
steps:
  - id: ping
    request: {method: FETCH, url: https://example.test/a}
`,
			wantErr: true,
			want:    []string{`unsupported HTTP method`},
		},
		{
			name: "unsupported version",
			yaml: `
version: "2"
name: demo
steps:
  - id: ping
    request: {method: GET, url: https://example.test/a}
`,
			wantErr: true,
			want:    []string{`unsupported version`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def, err := ParseBytes([]byte(tt.yaml))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			err = Validate(def)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, sub := range tt.want {
				if err == nil || !strings.Contains(err.Error(), sub) {
					t.Fatalf("error %v should contain %q", err, sub)
				}
			}
		})
	}
}

func TestCheckInputs(t *testing.T) {
	t.Parallel()
	optional := false
	def := &Definition{
		Inputs: map[string]InputSpec{
			"required": {Type: InputTypeString},
			"maybe":    {Type: InputTypeString, Required: &optional},
		},
	}
	err := CheckInputs(def, map[string]string{"required": "x"})
	if err != nil {
		t.Fatal(err)
	}
	err = CheckInputs(def, map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("got %v", err)
	}
	err = CheckInputs(def, map[string]string{"required": "x", "extra": "y"})
	if err == nil || !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("got %v", err)
	}
}
