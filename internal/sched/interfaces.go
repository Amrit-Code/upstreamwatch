package sched

import (
	"context"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// Runner starts one job by name and records the run. pipeline.Runner
// implements it.
type Runner interface {
	Run(ctx context.Context, job string, scope model.Scope) (model.Run, error)
}
