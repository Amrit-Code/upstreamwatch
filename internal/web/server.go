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

// defaultShutdownTimeout is how long Serve lets in-flight requests finish
// before it closes their connections.
const defaultShutdownTimeout = 10 * time.Second

// Options configures the web server.
type Options struct {
	Listen  string // host:port, for example 127.0.0.1:8080
	BaseURL string // the configured http.base_url; may be empty
}

// Server is the web UI: the findings page, the repo and timeline pages, the
// health endpoint and the stylesheet, behind the security-header middleware,
// the host check and the cross-origin check.
type Server struct {
	health   HealthReader
	findings FindingsReader
	timeline TimelineReader
	opts     Options
	logger   *slog.Logger
	pages    map[string]*template.Template
	inner    http.Handler // the mux behind the cross-origin check
	handler  http.Handler

	shutdownTimeout time.Duration // defaultShutdownTimeout; tests shorten it
}

// NewServer parses every embedded template and builds the handler. Requests
// are answered only for the hosts Options names: the Listen address (the
// loopback names instead when its host is unspecified, plus localhost when it
// is a loopback address) and the host of BaseURL. It returns an error when a
// template fails to parse or BaseURL is not an absolute http or https URL. It
// does not listen.
func NewServer(health HealthReader, findings FindingsReader, timeline TimelineReader, opts Options, logger *slog.Logger) (*Server, error) {
	pages, err := parsePages(templateFS)
	if err != nil {
		return nil, err
	}
	s := &Server{
		health:   health,
		findings: findings,
		timeline: timeline,
		opts:     opts,
		logger:   logger,
		pages:    pages,

		shutdownTimeout: defaultShutdownTimeout,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleFindings)
	mux.HandleFunc("GET /repos", s.handleRepos)
	mux.HandleFunc("GET /repos/{slug}", s.handleTimeline)
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
	s.inner = cop.Handler(mux)
	s.handler = s.withHostCheck(allowedHosts(opts.Listen, opts.BaseURL))
	return s, nil
}

// withHostCheck wraps the inner handler in the host check for allowed and in
// the security headers, which the host check's rejections carry too.
func (s *Server) withHostCheck(allowed map[string]struct{}) http.Handler {
	return securityHeaders(hostCheck(allowed, s.logger, s.inner))
}

// Handler returns the server's full handler, middleware included. Its host
// check allows the hosts Options names as given, so with a Listen port of 0
// it allows port 0, not the port Serve would listen on.
func (s *Server) Handler() http.Handler { return s.handler }

// Serve listens on Options.Listen and serves until ctx is cancelled, then
// shuts down, giving in-flight requests up to 10 seconds to finish. Requests
// still running after that, or when serving fails, have their connections
// closed, so no handler outlives Serve; the shutdown error is still
// returned. With a
// Listen port of 0 the allowed hosts are built from the address actually
// listened on, before the first request is served.
func (s *Server) Serve(ctx context.Context) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", s.opts.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.opts.Listen, err)
	}
	handler := s.handler
	if _, port, err := net.SplitHostPort(s.opts.Listen); err == nil && port == "0" {
		handler = s.withHostCheck(allowedHosts(ln.Addr().String(), s.opts.BaseURL))
	}
	srv := &http.Server{
		Handler:           handler,
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
		// Requests already accepted may still be running; drop them so no
		// handler outlives Serve.
		if closeErr := srv.Close(); closeErr != nil {
			return errors.Join(fmt.Errorf("serve http: %w", err), fmt.Errorf("close http server: %w", closeErr))
		}
		return fmt.Errorf("serve http: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.shutdownTimeout)
	defer cancel()
	shutdownErr := srv.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		// Shutdown gave up waiting (its deadline passed): close the
		// connections still active, which cancels their request contexts.
		if closeErr := srv.Close(); closeErr != nil {
			shutdownErr = errors.Join(shutdownErr, fmt.Errorf("close http server: %w", closeErr))
		}
	}
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
	for _, name := range []string{"findings", "repos", "timeline", "error"} {
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
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
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
