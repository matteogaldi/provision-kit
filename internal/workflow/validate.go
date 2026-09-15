package workflow

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

var (
	namePattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	stepIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
)

var allowedMethods = map[string]struct{}{
	http.MethodGet:    {},
	http.MethodPost:   {},
	http.MethodPut:    {},
	http.MethodPatch:  {},
	http.MethodDelete: {},
	http.MethodHead:   {},
}

func Validate(def *Definition) error {
	if def == nil {
		return Error{Message: "workflow definition is nil"}
	}

	var errs ErrorList
	validateStructure(def, &errs)

	ids := map[string]int{}
	for i, step := range def.Steps {
		if step.ID == "" {
			continue
		}
		if prev, exists := ids[step.ID]; exists {
			errs = append(errs, Error{
				Step:    step.ID,
				Message: fmt.Sprintf("duplicate step id (first defined at index %d, again at index %d)", prev, i),
			})
			continue
		}
		ids[step.ID] = i
	}

	for _, step := range def.Steps {
		for _, dep := range step.DependsOn {
			if dep == "" {
				errs = append(errs, Error{Step: step.ID, Field: "depends_on", Message: "dependency id is empty"})
				continue
			}
			if _, ok := ids[dep]; !ok {
				errs = append(errs, Error{
					Step:    step.ID,
					Field:   "depends_on",
					Message: fmt.Sprintf("unknown step %q", dep),
				})
			}
			if dep == step.ID {
				errs = append(errs, Error{
					Step:    step.ID,
					Field:   "depends_on",
					Message: "a step cannot depend on itself",
				})
			}
		}
	}

	if errs.Err() != nil {
		return errs.Err()
	}

	g, err := BuildGraph(def.Steps)
	if err != nil {
		return Error{Message: err.Error()}
	}
	def.Graph = g

	validateRefs(def, &errs)
	return errs.Err()
}

func validateStructure(def *Definition, errs *ErrorList) {
	if def.Version.String() == "" {
		*errs = append(*errs, Error{Field: "version", Message: "is required"})
	} else if def.Version.String() != CurrentVersion {
		*errs = append(*errs, Error{
			Field:   "version",
			Message: fmt.Sprintf("unsupported version %q (want %q)", def.Version.String(), CurrentVersion),
		})
	}

	if strings.TrimSpace(def.Name) == "" {
		*errs = append(*errs, Error{Field: "name", Message: "is required"})
	} else if !namePattern.MatchString(def.Name) {
		*errs = append(*errs, Error{
			Field:   "name",
			Message: "must match " + namePattern.String(),
		})
	}

	for name, spec := range def.Inputs {
		if !namePattern.MatchString(name) {
			*errs = append(*errs, Error{
				Field:   "inputs." + name,
				Message: "invalid input name",
			})
		}
		switch spec.Type {
		case "", InputTypeString:
			if spec.Type == "" {
				*errs = append(*errs, Error{
					Field:   "inputs." + name,
					Message: "type is required",
				})
			}
		default:
			*errs = append(*errs, Error{
				Field:   "inputs." + name,
				Message: fmt.Sprintf("unsupported type %q (want string)", spec.Type),
			})
		}
	}

	for name, url := range def.Services {
		if !namePattern.MatchString(name) {
			*errs = append(*errs, Error{Field: "services." + name, Message: "invalid service name"})
		}
		if strings.TrimSpace(url) == "" {
			*errs = append(*errs, Error{Field: "services." + name, Message: "url is required"})
		}
	}

	if len(def.Steps) == 0 {
		*errs = append(*errs, Error{Field: "steps", Message: "at least one step is required"})
	}

	for i, step := range def.Steps {
		prefix := fmt.Sprintf("steps[%d]", i)
		if step.ID == "" {
			*errs = append(*errs, Error{Field: prefix + ".id", Message: "is required"})
		} else if !stepIDPattern.MatchString(step.ID) {
			*errs = append(*errs, Error{Step: step.ID, Field: "id", Message: "must match " + stepIDPattern.String()})
		}

		method := strings.ToUpper(strings.TrimSpace(step.Request.Method))
		if method == "" {
			*errs = append(*errs, Error{Step: step.ID, Field: "request.method", Message: "is required"})
		} else if _, ok := allowedMethods[method]; !ok {
			*errs = append(*errs, Error{
				Step:    step.ID,
				Field:   "request.method",
				Message: fmt.Sprintf("unsupported HTTP method %q", step.Request.Method),
			})
		} else {
			def.Steps[i].Request.Method = method
		}

		if strings.TrimSpace(step.Request.URL) == "" {
			*errs = append(*errs, Error{Step: step.ID, Field: "request.url", Message: "is required"})
		}

		if step.Retry != nil {
			if step.Retry.Attempts < 1 {
				*errs = append(*errs, Error{Step: step.ID, Field: "retry.attempts", Message: "must be >= 1"})
			}
			switch step.Retry.Backoff {
			case "", BackoffNone, BackoffConstant, BackoffExponential:
				if step.Retry.Backoff == "" {
					*errs = append(*errs, Error{Step: step.ID, Field: "retry.backoff", Message: "is required when retry is set"})
				}
			default:
				*errs = append(*errs, Error{
					Step:    step.ID,
					Field:   "retry.backoff",
					Message: fmt.Sprintf("unsupported backoff %q (want none, constant, or exponential)", step.Retry.Backoff),
				})
			}
			if step.Retry.Delay < 0 {
				*errs = append(*errs, Error{Step: step.ID, Field: "retry.delay", Message: "must be >= 0"})
			}
		}
	}
}

func validateRefs(def *Definition, errs *ErrorList) {
	ids := make(map[string]struct{}, len(def.Steps))
	for _, step := range def.Steps {
		ids[step.ID] = struct{}{}
	}

	for _, step := range def.Steps {
		check := func(field string, value any) {
			refs, err := CollectRefs(value)
			if err != nil {
				*errs = append(*errs, Error{Step: step.ID, Field: field, Message: err.Error()})
				return
			}
			for _, ref := range refs {
				switch ref.Namespace {
				case NamespaceInputs:
					if _, ok := def.Inputs[ref.Name]; !ok {
						*errs = append(*errs, Error{
							Step:    step.ID,
							Field:   field,
							Message: fmt.Sprintf("unknown input %q", ref.Name),
						})
					}
				case NamespaceServices:
					if _, ok := def.Services[ref.Name]; !ok {
						*errs = append(*errs, Error{
							Step:    step.ID,
							Field:   field,
							Message: fmt.Sprintf("unknown service %q", ref.Name),
						})
					}
				case NamespaceSteps:
					if _, ok := ids[ref.Name]; !ok {
						*errs = append(*errs, Error{
							Step:    step.ID,
							Field:   field,
							Message: fmt.Sprintf("unknown step %q", ref.Name),
						})
						continue
					}
					if ref.Name == step.ID {
						*errs = append(*errs, Error{
							Step:    step.ID,
							Field:   field,
							Message: "cannot reference its own output",
						})
						continue
					}
					if def.Graph != nil && !def.Graph.DependsOnTransitively(step.ID, ref.Name) {
						*errs = append(*errs, Error{
							Step:    step.ID,
							Field:   field,
							Message: fmt.Sprintf("references step %q which is not a dependency", ref.Name),
						})
					}
				}
			}
		}

		check("request.url", step.Request.URL)
		for k, v := range step.Request.Headers {
			check("request.headers."+k, v)
		}
		check("request.body", step.Request.Body)
	}
}

func CheckInputs(def *Definition, inputs map[string]string) error {
	var errs ErrorList
	if inputs == nil {
		inputs = map[string]string{}
	}
	for name, spec := range def.Inputs {
		if spec.IsRequired() {
			if _, ok := inputs[name]; !ok {
				errs = append(errs, Error{
					Field:   "inputs." + name,
					Message: "is required",
				})
			}
		}
	}
	for name := range inputs {
		if _, ok := def.Inputs[name]; !ok {
			errs = append(errs, Error{
				Field:   "inputs." + name,
				Message: "is not declared",
			})
		}
	}
	return errs.Err()
}
