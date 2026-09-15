package workflow

import "fmt"

// Graph is a directed dependency graph. An edge A -> B means B depends on A
// (A must complete before B can run).
type Graph struct {
	Order      []string
	Deps       map[string][]string
	Dependents map[string][]string
}

func BuildGraph(steps []StepDef) (*Graph, error) {
	ids := make(map[string]struct{}, len(steps))
	orderHint := make([]string, 0, len(steps))
	deps := make(map[string][]string, len(steps))
	dependents := make(map[string][]string, len(steps))
	indegree := make(map[string]int, len(steps))

	for _, step := range steps {
		ids[step.ID] = struct{}{}
		orderHint = append(orderHint, step.ID)
		deps[step.ID] = append([]string{}, step.DependsOn...)
		dependents[step.ID] = nil
		indegree[step.ID] = 0
	}

	for _, step := range steps {
		seen := make(map[string]struct{}, len(step.DependsOn))
		for _, dep := range step.DependsOn {
			if _, ok := ids[dep]; !ok {
				return nil, fmt.Errorf("step %q: depends_on unknown step %q", step.ID, dep)
			}
			if _, dup := seen[dep]; dup {
				continue
			}
			seen[dep] = struct{}{}
			dependents[dep] = append(dependents[dep], step.ID)
			indegree[step.ID]++
		}
	}

	queue := make([]string, 0, len(steps))
	for _, id := range orderHint {
		if indegree[id] == 0 {
			queue = append(queue, id)
		}
	}

	order := make([]string, 0, len(steps))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, id)
		for _, next := range dependents[id] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if len(order) != len(steps) {
		leftover := make([]string, 0, len(steps)-len(order))
		done := make(map[string]struct{}, len(order))
		for _, id := range order {
			done[id] = struct{}{}
		}
		for _, id := range orderHint {
			if _, ok := done[id]; !ok {
				leftover = append(leftover, id)
			}
		}
		return nil, fmt.Errorf("circular dependency involving steps: %s", joinIDs(leftover))
	}

	return &Graph{
		Order:      order,
		Deps:       deps,
		Dependents: dependents,
	}, nil
}

func (g *Graph) Ready(completed map[string]bool) []string {
	if g == nil {
		return nil
	}
	var ready []string
	for _, id := range g.Order {
		if completed[id] {
			continue
		}
		ok := true
		for _, dep := range g.Deps[id] {
			if !completed[dep] {
				ok = false
				break
			}
		}
		if ok {
			ready = append(ready, id)
		}
	}
	return ready
}

func (g *Graph) DependsOnTransitively(step, target string) bool {
	if g == nil {
		return false
	}
	seen := map[string]struct{}{step: {}}
	stack := append([]string{}, g.Deps[step]...)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == target {
			return true
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		stack = append(stack, g.Deps[n]...)
	}
	return false
}

func joinIDs(ids []string) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += ", "
		}
		out += id
	}
	return out
}
