package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"

	"github.com/Amrit-Code/upstreamwatch/internal/config"
	"github.com/Amrit-Code/upstreamwatch/internal/gitsrc"
	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/issues/github"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln/osv"
)

const doctorSynopsis = `usage: upw doctor [-config path] [-log-level level]

Check that the config loads, the secrets are set, git is installed and the
outside services answer. Exits 1 if any check fails.
`

// Checker is a cheap reachability and auth probe that never mutates
// anything. Every outbound adapter implements it.
type Checker interface {
	Check(ctx context.Context) error
}

// Statuses of a doctor report line.
const (
	statusOK   = "ok"
	statusWarn = "warn"
	statusFail = "fail"
)

// namedCheck is one network check in the doctor report.
type namedCheck struct {
	name    string
	success string // the detail printed when the check passes
	checker Checker
}

// reportLine is one line of the doctor report.
type reportLine struct {
	status, name, detail string
}

// cmdDoctor loads the config and prints the doctor report built from the
// config and the checks newChecks returns. It does not open the database.
func cmdDoctor(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string,
	newChecks func(config.Config, *slog.Logger) []namedCheck) error {
	c := newCommand("doctor", doctorSynopsis, stdout, stderr)
	if err := c.parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(c.configPath, getenv)
	if err != nil {
		return err
	}
	return doctorReport(ctx, stdout, c.configPath, cfg, newChecks(cfg, c.logger()))
}

// doctorChecks returns the network checks in report order: git, OSV,
// deps.dev, then GitHub once per configured token, or once unauthenticated
// when neither token is set. The checks share one httpx client with no cache
// store, since none of them uses cached validators.
func doctorChecks(cfg config.Config, logger *slog.Logger) []namedCheck {
	hc := httpx.New(nil, httpx.Options{}, logger)
	const githubOK = "api.github.com reachable"
	checks := []namedCheck{
		{name: "git", success: "installed", checker: gitsrc.New(logger)},
		{name: "osv", success: "api.osv.dev reachable", checker: osv.New(hc, osv.Options{}, logger)},
		{name: "deps.dev", success: "api.deps.dev reachable", checker: depsdev.New(hc, depsdev.Options{}, logger)},
	}
	upstream, targets := cfg.Auth.GitHubUpstreamToken, cfg.Auth.GitHubTargetsToken
	if upstream != "" {
		checks = append(checks, namedCheck{name: "github (upstream token)", success: githubOK,
			checker: github.New(hc, upstream, github.Options{}, logger)})
	}
	if targets != "" {
		checks = append(checks, namedCheck{name: "github (targets token)", success: githubOK,
			checker: github.New(hc, targets, github.Options{}, logger)})
	}
	if upstream == "" && targets == "" {
		checks = append(checks, namedCheck{name: "github", success: githubOK,
			checker: github.New(hc, "", github.Options{}, logger)})
	}
	return checks
}

// doctorReport writes one line for the config, one per secret and one per
// check, running the checks one after another. A failed check does not stop
// the later ones. It returns errReported if any line is a failure.
func doctorReport(ctx context.Context, w io.Writer, configPath string, cfg config.Config, checks []namedCheck) error {
	failed := false
	write := func(l reportLine) {
		failed = failed || l.status == statusFail
		fmt.Fprintf(w, "%-5s %-23s %s\n", l.status, l.name, l.detail)
	}

	write(reportLine{statusOK, "config", configPath})
	for _, l := range secretLines(cfg) {
		write(l)
	}
	for _, c := range checks {
		if err := c.checker.Check(ctx); err != nil {
			write(reportLine{statusFail, c.name, err.Error()})
			continue
		}
		write(reportLine{statusOK, c.name, c.success})
	}

	if failed {
		return errReported
	}
	return nil
}

// secretLines returns the report lines for the secrets: both GitHub tokens,
// the LLM key when the LLM stage is enabled, and each notify channel that
// takes a secret. Values appear only in their redacted form.
func secretLines(cfg config.Config) []reportLine {
	lines := []reportLine{
		secretLine("github targets token", cfg.Auth.GitHubTargetsToken, "not set: private targets cannot be fetched"),
		secretLine("github upstream token", cfg.Auth.GitHubUpstreamToken, "not set: upstream polling limited to 60 requests an hour"),
	}
	if cfg.LLM.Enabled {
		lines = append(lines, secretLine("llm api key", cfg.LLM.APIKey, "not set: add it on the settings page or set UPW_LLM_API_KEY"))
	}
	for i, ch := range cfg.Notify.Channels {
		var secret model.Secret
		switch ch.Type {
		case "discord_webhook", "slack_webhook", "ntfy":
			secret = ch.URL
		case "email":
			secret = ch.Password
		default:
			continue
		}
		name := "notify channel " + strconv.Itoa(i+1)
		if secret == "" {
			lines = append(lines, reportLine{statusWarn, name, ch.Type + " secret not set"})
			continue
		}
		lines = append(lines, reportLine{statusOK, name, ch.Type + " " + secret.String()})
	}
	return lines
}

// secretLine is ok with the redacted value when s is set, and a warning with
// unset otherwise.
func secretLine(name string, s model.Secret, unset string) reportLine {
	if s == "" {
		return reportLine{statusWarn, name, unset}
	}
	return reportLine{statusOK, name, s.String()}
}
