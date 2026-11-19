package web

import "time"

// SetShutdownTimeout shortens how long Serve waits for in-flight requests, so
// a test can reach the shutdown deadline without waiting 10 seconds.
func SetShutdownTimeout(s *Server, d time.Duration) { s.shutdownTimeout = d }

// SetNow fixes the clock the pages use to work out a finding's effective
// state.
func SetNow(s *Server, now func() time.Time) { s.now = now }
