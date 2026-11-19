package web

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/style.css
var styleCSS []byte

// contentSecurityPolicy forbids scripts, inline styles and every external
// asset: the UI is plain HTML forms and one same-origin stylesheet.
const contentSecurityPolicy = "default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; frame-ancestors 'none'"

const shutdownTimeout = 10 * time.Second

// Options configures the web server.
type Options struct {
	Listen  string // host:port, for example 127.0.0.1:8080
	BaseURL string // the configured http.base_url; may be empty
}

// Server is the web UI: the findings page, the health endpoint and the
// stylesheet, behind the security-header middleware and the cross-origin
// check.
type Server struct {
	health   HealthReader
	findings FindingsReader
	opts     Options
	logger   *slog.Logger
	pages    map[string]*template.Template
	handler  http.Handler
}

// NewServer parses every embedded template and builds the handler. It returns
// an error when a template fails to parse or BaseURL is not an absolute http
// or https URL. It does not listen.
func NewServer(health HealthReader, findings FindingsReader, opts Options, logger *slog.Logger) (*Server, error) {
	pages, err := parsePages(templateFS)
	if err != nil {
		return nil, err
	}
	s := &Server{
		health:   health,
		findings: findings,
		opts:     opts,
		logger:   logger,
		pages:    pages,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleFindings)
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /static/style.css", s.handleStyle)

	cop := http.NewCrossOriginProtection()
	if opts.BaseURL != "" {
		origin, err := baseOrigin(opts.BaseURL)
		if err != nil {
			return nil, err
		}
		if err := cop.AddTrustedOrigin(origin); err != nil {
			return nil, fmt.Errorf("trust base url origin: %w", err)
		}
	}
	s.handler = securityHeaders(cop.Handler(mux))
	return s, nil
}

// Handler returns the server's full handler, middleware included.
func (s *Server) Handler() http.Handler { return s.handler }

// Serve listens on Options.Listen and serves until ctx is cancelled, then
// shuts down, giving in-flight requests up to 10 seconds to finish.
func (s *Server) Serve(ctx context.Context) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", s.opts.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.opts.Listen, err)
	}
	srv := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		// Route net/http's own diagnostics through the injected logger
		// instead of the log package.
		ErrorLog: slog.NewLogLogger(s.logger.Handler(), slog.LevelWarn),
	}
	s.logger.Info("http server listening", "addr", ln.Addr().String())

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve http: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	shutdownErr := srv.Shutdown(shutdownCtx)
	// Serve returns ErrServerClosed as soon as Shutdown starts, so this
	// receive does not wait for in-flight requests.
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve http: %w", err)
	}
	if shutdownErr != nil {
		return fmt.Errorf("shut down http server: %w", shutdownErr)
	}
	return nil
}

// parsePages parses the layout into a base set and, for each page, parses the
// page file into a clone of it, because every page defines "content".
func parsePages(fsys fs.FS) (map[string]*template.Template, error) {
	base, err := template.ParseFS(fsys, "templates/layout.html")
	if err != nil {
		return nil, fmt.Errorf("parse layout template: %w", err)
	}
	pages := make(map[string]*template.Template)
	for _, name := range []string{"findings"} {
		clone, err := base.Clone()
		if err != nil {
			return nil, fmt.Errorf("clone layout for %s: %w", name, err)
		}
		page, err := clone.ParseFS(fsys, "templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("parse %s template: %w", name, err)
		}
		pages[name] = page
	}
	return pages, nil
}

// baseOrigin returns the scheme://host[:port] origin of the configured base
// URL.
func baseOrigin(baseURL string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("base url: must be an absolute http or https URL")
	}
	return u.Scheme + "://" + u.Host, nil
}

// securityHeaders sets the content security policy and nosniff on every
// response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}
