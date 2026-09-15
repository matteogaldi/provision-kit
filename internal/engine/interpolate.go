package engine

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/matteogaldi/provision-kit/internal/store"
	"github.com/matteogaldi/provision-kit/internal/workflow"
)

type Runtime struct {
	Inputs   map[string]string
	Services map[string]string
	Steps    map[string]*store.StepState
}

func Interpolate(value any, rt Runtime) (any, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case string:
		return interpolateString(v, rt)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			iv, err := Interpolate(item, rt)
			if err != nil {
				return nil, err
			}
			out[i] = iv
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			iv, err := Interpolate(item, rt)
			if err != nil {
				return nil, err
			}
			out[k] = iv
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			iv, err := Interpolate(item, rt)
			if err != nil {
				return nil, err
			}
			out[fmt.Sprint(k)] = iv
		}
		return out, nil
	default:
		return value, nil
	}
}

func interpolateString(s string, rt Runtime) (any, error) {
	refs, err := workflow.FindRefs(s)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return s, nil
	}
	if strings.TrimSpace(s) == refs[0].Raw && len(refs) == 1 {
		return lookup(refs[0], rt)
	}
	out := s
	for _, ref := range refs {
		val, err := lookup(ref, rt)
		if err != nil {
			return nil, err
		}
		out = strings.Replace(out, ref.Raw, stringify(val), 1)
	}
	return out, nil
}

func lookup(ref workflow.Ref, rt Runtime) (any, error) {
	switch ref.Namespace {
	case workflow.NamespaceInputs:
		v, ok := rt.Inputs[ref.Name]
		if !ok {
			return nil, fmt.Errorf("unknown input %q", ref.Name)
		}
		return v, nil
	case workflow.NamespaceServices:
		v, ok := rt.Services[ref.Name]
		if !ok {
			return nil, fmt.Errorf("unknown service %q", ref.Name)
		}
		return v, nil
	case workflow.NamespaceSteps:
		st, ok := rt.Steps[ref.Name]
		if !ok {
			return nil, fmt.Errorf("unknown step %q", ref.Name)
		}
		if st == nil || st.Status != store.StatusSucceeded {
			return nil, fmt.Errorf("step %q has no output yet", ref.Name)
		}
		return dig(st.Output, ref.Path[1:], ref.Expression())
	default:
		return nil, fmt.Errorf("unknown namespace %q", ref.Namespace)
	}
}

func dig(v any, path []string, expr string) (any, error) {
	cur := v
	for _, key := range path {
		if cur == nil {
			return nil, fmt.Errorf("%s: output has no field %q", expr, key)
		}
		m, ok := asMap(cur)
		if !ok {
			return nil, fmt.Errorf("%s: cannot read field %q from %T", expr, key, cur)
		}
		next, ok := m[key]
		if !ok {
			return nil, fmt.Errorf("%s: output has no field %q", expr, key)
		}
		cur = next
	}
	return cur, nil
}

func asMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			out[fmt.Sprint(k)] = val
		}
		return out, true
	default:
		return nil, false
	}
}

func stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprint(x)
		}
		return string(b)
	}
}
