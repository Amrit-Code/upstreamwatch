package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// validate returns one error per problem, each starting with the YAML path of
// the field, so the operator can fix them in one pass. Channels are checked
// by validateChannel, which needs the raw secret values.
func (c Config) validate() []error {
	var errs []error
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if c.DataDir == "" {
		add("data_dir: required")
	}

	if c.HTTP.Listen == "" {
		add("http.listen: required")
	} else if err := checkHostPort(c.HTTP.Listen); err != nil {
		add("http.listen: %v", err)
	}
	if c.HTTP.BaseURL != "" {
		u, err := url.Parse(c.HTTP.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
			add("http.base_url: must be an absolute http or https URL")
		}
	}

	slugs := make(map[string]int, len(c.Targets))
	for i, t := range c.Targets {
		path := fmt.Sprintf("targets[%d]", i)
		switch {
		case t.Slug == "":
			add("%s.slug: required", path)
		case !slugPattern.MatchString(t.Slug):
			add("%s.slug: %q must match %s", path, t.Slug, slugPattern)
		default:
			if first, dup := slugs[t.Slug]; dup {
				add("%s.slug: %q already used by targets[%d]", path, t.Slug, first)
			} else {
				slugs[t.Slug] = i
			}
		}
		errs = append(errs, checkTargetURL(path+".url", t.URL)...)
		if !slices.Contains([]TargetAuth{"targets", "upstream", "none"}, t.Auth) {
			add("%s.auth: %q is not one of targets, upstream, none", path, t.Auth)
		}
		if err := checkTokenHost(t.Auth, t.URL); err != nil {
			add("%s.auth: %v", path, err)
		}
		for j, p := range t.ExcludePaths {
			if err := checkExcludePath(p); err != nil {
				add("%s.exclude_paths[%d]: %v", path, j, err)
			}
		}
	}

	for _, s := range []struct{ key, expr string }{
		{"schedules.ingest", c.Schedules.Ingest},
		{"schedules.vulns", c.Schedules.Vulns},
		{"schedules.issues", c.Schedules.Issues},
		{"schedules.llm", c.Schedules.LLM},
		{"schedules.digest", c.Schedules.Digest},
		{"schedules.backup", c.Schedules.Backup},
	} {
		if !isCronShape(s.expr) {
			add("%s: %q is not a five-field cron expression", s.key, s.expr)
		}
	}

	if !slices.Contains([]string{"published", "pin"}, c.Vulns.ExposureBasis) {
		add("vulns.exposure_basis: %q is not one of published, pin", c.Vulns.ExposureBasis)
	}

	if c.Prefilter.Threshold <= 0 {
		add("prefilter.threshold: must be positive, got %d", c.Prefilter.Threshold)
	}

	if !slices.Contains([]string{"anthropic", "openai_compat"}, c.LLM.Provider) {
		add("llm.provider: %q is not one of anthropic, openai_compat", c.LLM.Provider)
	}
	if !slices.Contains([]string{"symbols", "snippets", "none"}, c.LLM.SnippetMode) {
		add("llm.snippet_mode: %q is not one of symbols, snippets, none", c.LLM.SnippetMode)
	}
	if c.LLM.Enabled && c.LLM.Model == "" {
		add("llm.model: required when llm.enabled is true")
	}

	for _, n := range []struct {
		key string
		v   float64
	}{
		{"upstream.backfill_days", float64(c.Upstream.BackfillDays)},
		{"upstream.backfill_max_issues", float64(c.Upstream.BackfillMaxIssues)},
		{"llm.max_snippets", float64(c.LLM.MaxSnippets)},
		{"llm.max_issue_chars", float64(c.LLM.MaxIssueChars)},
		{"llm.max_calls_per_run", float64(c.LLM.MaxCallsPerRun)},
		{"llm.max_usd_per_day", c.LLM.MaxUSDPerDay},
		{"llm.prices_per_mtok.input", c.LLM.PricesPerMTok.Input},
		{"llm.prices_per_mtok.output", c.LLM.PricesPerMTok.Output},
		{"retention.backups_keep", float64(c.Retention.BackupsKeep)},
		{"retention.issues_closed_keep_days", float64(c.Retention.IssuesClosedKeepDays)},
	} {
		if n.v < 0 {
			add("%s: must not be negative, got %v", n.key, n.v)
		}
	}

	if !isCronShape(c.Notify.Heartbeat) {
		add("notify.heartbeat: %q is not a five-field cron expression", c.Notify.Heartbeat)
	}

	if c.Git.MaxCloneMB < 1 {
		add("git.max_clone_mb: must be at least 1, got %d", c.Git.MaxCloneMB)
	}
	if c.Git.MaxBlobMB < 1 {
		add("git.max_blob_mb: must be at least 1, got %d", c.Git.MaxBlobMB)
	}

	return errs
}

// positiveDuration parses s as a Go duration such as 30m and requires it to
// be positive. Errors start with field.
func positiveDuration(field, s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a duration such as 30m or 1h30m", field, s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s: must be positive, got %s", field, s)
	}
	return d, nil
}

// checkHostPort reports whether s parses as host:port with a numeric port.
func checkHostPort(s string) error {
	_, port, err := net.SplitHostPort(s)
	if err != nil {
		return fmt.Errorf("must be host:port: %w", err)
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return fmt.Errorf("port %q must be a number from 0 to 65535", port)
	}
	return nil
}

// checkTargetURL applies the static rules for a target clone URL: https, a
// host and no user info. The address checks are done before every fetch, not
// here. Errors never repeat the URL, which could carry a credential.
func checkTargetURL(path, raw string) []error {
	if raw == "" {
		return []error{fmt.Errorf("%s: required", path)}
	}
	u, err := url.Parse(raw)
	if err != nil {
		// url.Error repeats the whole URL; keep only its reason.
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return []error{fmt.Errorf("%s: invalid URL: %w", path, err)}
	}
	var errs []error
	if u.Scheme != "https" {
		errs = append(errs, fmt.Errorf("%s: scheme %q not allowed, only https", path, u.Scheme))
	}
	if u.Host == "" {
		errs = append(errs, fmt.Errorf("%s: host required", path))
	}
	if u.User != nil {
		errs = append(errs, fmt.Errorf("%s: must not contain user info; credentials come from auth", path))
	}
	return errs
}

// tokenHost is the only host a GitHub token is sent to.
const tokenHost = "github.com"

// checkTokenHost rejects a target that would send a GitHub token (auth
// targets or upstream) to a host other than github.com. A URL that does not
// parse is left to checkTargetURL. The error names the host only, never the
// URL, which could carry a credential.
func checkTokenHost(auth TargetAuth, raw string) error {
	if auth != "targets" && auth != "upstream" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil
	}
	if host := u.Hostname(); !strings.EqualFold(host, tokenHost) {
		return fmt.Errorf("%s sends a GitHub token, so the host must be %s, got %q; use auth none for other hosts", auth, tokenHost, host)
	}
	return nil
}

// checkExcludePath applies the rules for one targets[].exclude_paths entry:
// a non-empty relative slash-separated path with no leading slash and no ".."
// segment.
func checkExcludePath(p string) error {
	switch {
	case p == "":
		return errors.New("must not be empty")
	case strings.HasPrefix(p, "/"):
		return fmt.Errorf("%q must be relative to the repository root, without a leading /", p)
	case slices.Contains(strings.Split(p, "/"), ".."):
		return fmt.Errorf("%q must not contain a .. segment", p)
	}
	return nil
}

// isCronShape reports whether s has the shape of a five-field cron
// expression: five whitespace-separated fields made of digits, '*', '/', ','
// and '-'. The scheduler parses it properly.
func isCronShape(s string) bool {
	fields := strings.Fields(s)
	if len(fields) != 5 {
		return false
	}
	for _, f := range fields {
		if strings.Trim(f, "0123456789*/,-") != "" {
			return false
		}
	}
	return true
}

// validateChannel checks one notify channel. raw is the channel as written,
// so a secret field counts as present when the file sets it, even if the
// variable it references is unset when the config is loaded.
func validateChannel(path string, raw fileChannel, ch Channel) []error {
	var errs []error
	required := func(field string, missing bool) {
		if missing {
			errs = append(errs, fmt.Errorf("%s.%s: required for type %s", path, field, ch.Type))
		}
	}
	switch ch.Type {
	case "discord_webhook", "slack_webhook", "ntfy":
		required("url", raw.URL == "")
	case "email":
		required("host", ch.Host == "")
		if ch.Port < 1 || ch.Port > 65535 {
			errs = append(errs, fmt.Errorf("%s.port: must be from 1 to 65535, got %d", path, ch.Port))
		}
		required("username", ch.Username == "")
		required("password", raw.Password == "")
		required("from", ch.From == "")
		required("to", len(ch.To) == 0)
		for i, addr := range ch.To {
			if addr == "" {
				errs = append(errs, fmt.Errorf("%s.to[%d]: must not be empty", path, i))
			}
		}
	case "stdout":
		// stdout takes no fields.
	case "":
		errs = append(errs, fmt.Errorf("%s.type: required", path))
	default:
		errs = append(errs, fmt.Errorf("%s.type: unknown type %q, want one of email, discord_webhook, slack_webhook, ntfy, stdout", path, ch.Type))
	}
	return errs
}
