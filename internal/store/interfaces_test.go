package store_test

import (
	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/ingest"
	"github.com/Amrit-Code/upstreamwatch/internal/pipeline"
	"github.com/Amrit-Code/upstreamwatch/internal/query"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
	"github.com/Amrit-Code/upstreamwatch/internal/timeline"
)

// The store satisfies every store interface its consumers declare.
var (
	_ pipeline.RunStore      = (*store.DB)(nil)
	_ pipeline.FindingStore  = (*store.DB)(nil)
	_ query.RunStore         = (*store.DB)(nil)
	_ query.FindingStore     = (*store.DB)(nil)
	_ httpx.CacheStore       = (*store.DB)(nil)
	_ ingest.RepoStore       = (*store.DB)(nil)
	_ timeline.RepoStore     = (*store.DB)(nil)
	_ timeline.TimelineStore = (*store.DB)(nil)
)
