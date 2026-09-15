package workflow

import (
	"fmt"
	"strings"
)

type Error struct {
	Step    string
	Field   string
	Message string
}

func (e Error) Error() string {
	switch {
	case e.Step != "" && e.Field != "":
		return fmt.Sprintf("step %q: %s: %s", e.Step, e.Field, e.Message)
	case e.Step != "":
		return fmt.Sprintf("step %q: %s", e.Step, e.Message)
	case e.Field != "":
		return fmt.Sprintf("%s: %s", e.Field, e.Message)
	default:
		return e.Message
	}
}

type ErrorList []error

func (e ErrorList) Error() string {
	if len(e) == 0 {
		return ""
	}
	if len(e) == 1 {
		return e[0].Error()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d errors:", len(e))
	for _, err := range e {
		b.WriteString("\n  - ")
		b.WriteString(err.Error())
	}
	return b.String()
}

func (e ErrorList) Err() error {
	if len(e) == 0 {
		return nil
	}
	return e
}
