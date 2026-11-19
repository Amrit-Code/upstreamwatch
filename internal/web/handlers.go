package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"
)

// healthResponse is the body of GET /health.
type healthResponse struct {
	OK       bool        `json:"ok"`
	LastRuns []healthRun `json:"last_runs"`
}

// healthRun is one job's latest run in GET /health. FinishedAt is nil while
// the run is still running.
type healthRun struct {
	Job        string  `json:"job"`
	Status     string  `json:"status"`
	StartedAt  string  `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
	Error      string  `json:"error"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	runs, err := s.health.LastRuns(r.Context())
	if err != nil {
		s.logger.Error("health check failed", "error", err.Error())
		s.writeJSON(w, http.StatusServiceUnavailable, healthResponse{OK: false, LastRuns: []healthRun{}})
		return
	}
	resp := healthResponse{OK: true, LastRuns: make([]healthRun, 0, len(runs))}
	for _, run := range runs {
		hr := healthRun{
			Job:       run.Job,
			Status:    string(run.Status),
			StartedAt: formatRFC3339(run.StartedAt),
			Error:     run.Error,
		}
		if !run.FinishedAt.IsZero() {
			finished := formatRFC3339(run.FinishedAt)
			hr.FinishedAt = &finished
		}
		resp.LastRuns = append(resp.LastRuns, hr)
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// formatRFC3339 formats t in UTC to whole seconds.
func formatRFC3339(t time.Time) string {
	return t.UTC().Truncate(time.Second).Format(time.RFC3339)
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		s.logger.Error("encode json response failed", "error", err.Error())
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status is already sent; a write error means the client went away.
	_, _ = w.Write(buf.Bytes())
}

// render executes a page into a buffer first, so a template failure becomes a
// clean 500 instead of half a page sent with 200.
func (s *Server) render(w http.ResponseWriter, status int, name string, data any) {
	var buf bytes.Buffer
	if err := s.pages[name].ExecuteTemplate(&buf, "layout", data); err != nil {
		s.logger.Error("render page failed", "page", name, "error", err.Error())
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	// The status is already sent; a write error means the client went away.
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) handleStyle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	// A write error means the client went away.
	_, _ = w.Write(styleCSS)
}
