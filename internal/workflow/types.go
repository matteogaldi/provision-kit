package workflow

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

const CurrentVersion = "1"

type InputType string

const (
	InputTypeString InputType = "string"
)

type Backoff string

const (
	BackoffNone        Backoff = "none"
	BackoffConstant    Backoff = "constant"
	BackoffExponential Backoff = "exponential"
)

// ScalarString accepts a YAML scalar whether it is quoted or not
// (for example version: 1 and version: "1").
type ScalarString string

func (s ScalarString) String() string { return string(s) }

func (s *ScalarString) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("expected a scalar")
	}
	*s = ScalarString(value.Value)
	return nil
}

type Definition struct {
	Version  ScalarString         `yaml:"version"`
	Name     string               `yaml:"name"`
	Services map[string]string    `yaml:"services"`
	Inputs   map[string]InputSpec `yaml:"inputs"`
	Steps    []StepDef            `yaml:"steps"`

	Graph *Graph `yaml:"-"`
}

type InputSpec struct {
	Type     InputType `yaml:"type"`
	Required *bool     `yaml:"required,omitempty"`
}

func (s InputSpec) IsRequired() bool {
	if s.Required == nil {
		return true
	}
	return *s.Required
}

type StepDef struct {
	ID        string         `yaml:"id"`
	DependsOn []string       `yaml:"depends_on"`
	Request   HTTPRequestDef `yaml:"request"`
	Retry     *RetrySpec     `yaml:"retry,omitempty"`
}

func (s StepDef) RetryOrDefault() RetrySpec {
	if s.Retry == nil {
		return RetrySpec{Attempts: 1, Backoff: BackoffNone}
	}
	out := *s.Retry
	if out.Attempts < 1 {
		out.Attempts = 1
	}
	if out.Backoff == "" {
		out.Backoff = BackoffNone
	}
	return out
}

type HTTPRequestDef struct {
	Method  string            `yaml:"method"`
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"`
	Body    any               `yaml:"body"`
}

type RetrySpec struct {
	Attempts int      `yaml:"attempts"`
	Backoff  Backoff  `yaml:"backoff"`
	Delay    Duration `yaml:"delay"`
}

type Duration time.Duration

func (d Duration) Duration() time.Duration { return time.Duration(d) }

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("delay must be a duration string")
	}
	if value.Value == "" {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("invalid delay %q: %w", value.Value, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Definition) Step(id string) (StepDef, bool) {
	for _, step := range d.Steps {
		if step.ID == id {
			return step, true
		}
	}
	return StepDef{}, false
}

func (d Definition) StepIDs() []string {
	ids := make([]string, 0, len(d.Steps))
	for _, step := range d.Steps {
		ids = append(ids, step.ID)
	}
	return ids
}
