package engine

import (
	"time"

	"github.com/matteogaldi/provision-kit/internal/workflow"
)

const defaultDelay = 100 * time.Millisecond

func backoffDelay(spec workflow.RetrySpec, attempt int) time.Duration {
	base := spec.Delay.Duration()
	if base <= 0 {
		base = defaultDelay
	}
	switch spec.Backoff {
	case workflow.BackoffConstant:
		return base
	case workflow.BackoffExponential:
		d := base
		for i := 1; i < attempt; i++ {
			d *= 2
			if d > 2*time.Second {
				return 2 * time.Second
			}
		}
		return d
	default:
		return 0
	}
}
