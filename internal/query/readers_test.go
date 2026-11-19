package query_test

import (
	"github.com/Amrit-Code/upstreamwatch/internal/query"
	"github.com/Amrit-Code/upstreamwatch/internal/web"
)

var (
	_ web.HealthReader   = (*query.Service)(nil)
	_ web.FindingsReader = (*query.Service)(nil)
)
