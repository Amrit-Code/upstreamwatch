package store_test

import (
	"github.com/Amrit-Code/upstreamwatch/internal/findings"
	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/ingest"
	"github.com/Amrit-Code/upstreamwatch/internal/ops"
	"github.com/Amrit-Code/upstreamwatch/internal/pipeline"
	"github.com/Amrit-Code/upstreamwatch/internal/query"
	"github.com/Amrit-Code/upstreamwatch/internal/report"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
	"github.com/Amrit-Code/upstreamwatch/internal/timeline"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln"
)

// The store satisfies every store interface its consumers declare. cmd/upw
// passes *store.DB to each of these constructors, so a method that drifts
// from its interface fails here, in the store's own package tests.
var (
	_ pipeline.RunStore      = (*store.DB)(nil)
	_ pipeline.FindingStore  = (*store.DB)(nil)
	_ query.RunStore         = (*store.DB)(nil)
	_ query.TimelineStore    = (*store.DB)(nil)
	_ query.FindingStore     = (*store.DB)(nil)
	_ httpx.CacheStore       = (*store.DB)(nil)
	_ ingest.RepoStore       = (*store.DB)(nil)
	_ timeline.RepoStore     = (*store.DB)(nil)
	_ timeline.TimelineStore = (*store.DB)(nil)

	// M2 consumers.
	_ vuln.AdvisoryStore = (*store.DB)(nil)
	_ vuln.KVStore       = (*store.DB)(nil)
	_ vuln.FindingStore  = (*store.DB)(nil)
	_ report.Store       = (*store.DB)(nil)
	_ findings.Store     = (*store.DB)(nil)
	_ ops.Backuper       = (*store.DB)(nil)
)
