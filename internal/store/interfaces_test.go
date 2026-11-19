package store_test

import (
	"github.com/Amrit-Code/upstreamwatch/internal/pipeline"
	"github.com/Amrit-Code/upstreamwatch/internal/query"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
)

// The store satisfies every store interface its consumers declare.
var (
	_ pipeline.RunStore     = (*store.DB)(nil)
	_ pipeline.FindingStore = (*store.DB)(nil)
	_ query.RunStore        = (*store.DB)(nil)
	_ query.FindingStore    = (*store.DB)(nil)
)
