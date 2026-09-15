package workflow

import (
	"fmt"
	"regexp"
	"strings"
)

var refPattern = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

type Namespace string

const (
	NamespaceInputs   Namespace = "inputs"
	NamespaceServices Namespace = "services"
	NamespaceSteps    Namespace = "steps"
)

type Ref struct {
	Raw       string
	Namespace Namespace
	Name      string   // input name, service name, or step id
	Path      []string // for steps: segments after the step id (must start with "output")
}

func FindRefs(s string) ([]Ref, error) {
	matches := refPattern.FindAllStringSubmatch(s, -1)
	if matches == nil {
		return nil, nil
	}
	refs := make([]Ref, 0, len(matches))
	for _, m := range matches {
		ref, err := ParseRef(strings.TrimSpace(m[1]))
		if err != nil {
			return nil, err
		}
		ref.Raw = m[0]
		refs = append(refs, ref)
	}
	return refs, nil
}

func ParseRef(expr string) (Ref, error) {
	parts := strings.Split(expr, ".")
	if len(parts) == 0 || parts[0] == "" {
		return Ref{}, fmt.Errorf("empty interpolation expression")
	}
	switch Namespace(parts[0]) {
	case NamespaceInputs:
		if len(parts) != 2 || parts[1] == "" {
			return Ref{}, fmt.Errorf("interpolation %q: inputs references must be inputs.<name>", expr)
		}
		return Ref{Namespace: NamespaceInputs, Name: parts[1]}, nil
	case NamespaceServices:
		if len(parts) != 2 || parts[1] == "" {
			return Ref{}, fmt.Errorf("interpolation %q: services references must be services.<name>", expr)
		}
		return Ref{Namespace: NamespaceServices, Name: parts[1]}, nil
	case NamespaceSteps:
		if len(parts) < 3 || parts[1] == "" {
			return Ref{}, fmt.Errorf("interpolation %q: step references must be steps.<id>.output[.<field>...]", expr)
		}
		if parts[2] != "output" {
			return Ref{}, fmt.Errorf("interpolation %q: step references must use .output", expr)
		}
		return Ref{
			Namespace: NamespaceSteps,
			Name:      parts[1],
			Path:      parts[2:],
		}, nil
	default:
		return Ref{}, fmt.Errorf("interpolation %q: unknown namespace %q (want inputs, services, or steps)", expr, parts[0])
	}
}

func WalkStrings(v any, fn func(s string) error) error {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		return fn(x)
	case []any:
		for _, item := range x {
			if err := WalkStrings(item, fn); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, item := range x {
			if err := WalkStrings(item, fn); err != nil {
				return err
			}
		}
	case map[any]any:
		for _, item := range x {
			if err := WalkStrings(item, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

func CollectRefs(v any) ([]Ref, error) {
	var refs []Ref
	err := WalkStrings(v, func(s string) error {
		found, err := FindRefs(s)
		if err != nil {
			return err
		}
		refs = append(refs, found...)
		return nil
	})
	return refs, err
}

func (r Ref) Expression() string {
	switch r.Namespace {
	case NamespaceInputs, NamespaceServices:
		return string(r.Namespace) + "." + r.Name
	case NamespaceSteps:
		return "steps." + r.Name + "." + strings.Join(r.Path, ".")
	default:
		return r.Raw
	}
}
