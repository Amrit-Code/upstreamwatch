package main

import (
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/config"
	"github.com/Amrit-Code/upstreamwatch/internal/extract"
	"github.com/Amrit-Code/upstreamwatch/internal/extract/unity"
	"github.com/Amrit-Code/upstreamwatch/internal/gitsrc"
	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/ingest"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/ops"
	"github.com/Amrit-Code/upstreamwatch/internal/pipeline"
	"github.com/Amrit-Code/upstreamwatch/internal/report"
	"github.com/Amrit-Code/upstreamwatch/internal/sched"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
	"github.com/Amrit-Code/upstreamwatch/internal/timeline"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln/epss"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln/kev"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln/osv"
)

// vulnEndpoints holds the endpoint options of the vulns job's outside
// sources. Its zero value means each adapter's production defaults.
type vulnEndpoints struct {
	osv  osv.Options
	kev  kev.Options
	epss epss.Options
	// allowPrivateHosts exempts these hosts from the jobs' httpx address
	// and scheme policy, so a local test server can stand in for a source.
	allowPrivateHosts []string
}

// vulnSources is the endpoint set registeredJobs builds the vulns job's
// adapters with. Production leaves it zero; tests in this package point it
// at a local server, so no unit test reaches the internet.
var vulnSources vulnEndpoints

// registeredJobs returns every job the Runner knows, in pipeline order:
// ingest, timeline, vulns, digest, backup. Ingest and timeline share one git
// client; the HTTP adapters share one httpx client that keeps its validators
// in the store. Every job runs under the one lock file
// <data_dir>/locks/pipeline.lock, so no two jobs, from any processes on one
// data directory, run at once. Constructing them does no I/O. It returns an error, naming the channel and
// never its secret, when a notify channel cannot be built.
func registeredJobs(cfg config.Config, db *store.DB, logger *slog.Logger) ([]pipeline.Job, error) {
	git := gitsrc.New(gitOptions(cfg), logger)
	hc := httpx.New(db, httpx.Options{AllowPrivateHosts: vulnSources.allowPrivateHosts}, logger)
	extractors := []timeline.Extractor{
		extract.New(extract.Options{DeriveToolchainFromImages: cfg.Vulns.DeriveToolchainFromImages}, logger),
	}
	channels, err := buildChannels(cfg.Notify.Channels, hc, notifyStdout, logger)
	if err != nil {
		return nil, err
	}
	return withPipelineLock(filepath.Join(cfg.DataDir, "locks"), []pipeline.Job{
		ingest.NewService(git, db, ingest.Options{Targets: ingestTargets(cfg)}, logger),
		timeline.NewService(git, db, db, extractors, unity.New(), timeline.Options{Targets: timelineTargets(cfg)}, logger),
		vuln.NewService(
			osv.New(hc, vulnSources.osv, logger),
			kev.New(hc, vulnSources.kev, logger),
			epss.New(hc, vulnSources.epss, logger),
			db, db, db,
			vuln.Options{ExposureBasis: cfg.Vulns.ExposureBasis, EcosystemsExtra: cfg.Vulns.EcosystemsExtra},
			logger),
		report.NewService(db, channels, report.Options{
			BaseURL:           strings.TrimRight(cfg.HTTP.BaseURL, "/"),
			HeartbeatLastFire: heartbeatLastFire(cfg.Notify.Heartbeat, logger),
		}, logger),
		ops.NewBackup(db, ops.BackupOptions{Dir: filepath.Join(cfg.DataDir, "backups"), Keep: cfg.Retention.BackupsKeep}, logger),
	}), nil
}

// heartbeatLastFire returns the digest job's heartbeat function: the most
// recent fire time of the cron expression at or before now. When the
// expression does not parse it logs the problem and reports no heartbeat.
func heartbeatLastFire(expr string, logger *slog.Logger) func(now time.Time) (time.Time, bool) {
	return func(now time.Time) (time.Time, bool) {
		t, ok, err := sched.LastFire(expr, now)
		if err != nil {
			logger.Warn("heartbeat schedule not usable, no heartbeat sent", "cron", expr, "error", err.Error())
			return time.Time{}, false
		}
		return t, ok
	}
}

// gitOptions returns the git client settings: clones under <data_dir>/repos,
// git's HOME at <data_dir>/git-home, the clone and blob size caps and the
// timeouts from the git config section, and no file:// URLs. LookupHost is
// left nil so the client uses the system resolver for the private-address
// check.
func gitOptions(cfg config.Config) gitsrc.Options {
	return gitsrc.Options{
		ReposDir:      filepath.Join(cfg.DataDir, "repos"),
		HomeDir:       filepath.Join(cfg.DataDir, "git-home"),
		MaxCloneBytes: int64(cfg.Git.MaxCloneMB) << 20,
		MaxBlobBytes:  int64(cfg.Git.MaxBlobMB) << 20,
		CloneTimeout:  cfg.Git.CloneTimeout,
		FetchTimeout:  cfg.Git.FetchTimeout,
		AllowFileURLs: false,
		LookupHost:    nil,
	}
}

// ingestTargets returns one TargetRepo per configured target, in config
// order, carrying the token its auth setting names: the targets token, the
// upstream token, or none.
func ingestTargets(cfg config.Config) []model.TargetRepo {
	targets := make([]model.TargetRepo, 0, len(cfg.Targets))
	for _, t := range cfg.Targets {
		var token model.Secret
		switch t.Auth {
		case "targets":
			token = cfg.Auth.GitHubTargetsToken
		case "upstream":
			token = cfg.Auth.GitHubUpstreamToken
		}
		targets = append(targets, model.TargetRepo{
			Slug:             t.Slug,
			URL:              t.URL,
			Branch:           t.Branch,
			Token:            token,
			AllowPrivateHost: t.AllowPrivateHosts,
		})
	}
	return targets
}

// timelineTargets returns the timeline settings of each configured target,
// in config order.
func timelineTargets(cfg config.Config) []timeline.TargetSettings {
	targets := make([]timeline.TargetSettings, 0, len(cfg.Targets))
	for _, t := range cfg.Targets {
		targets = append(targets, timeline.TargetSettings{
			Slug:         t.Slug,
			Unity:        t.Unity,
			Ecosystems:   t.Ecosystems,
			ExcludePaths: t.ExcludePaths,
		})
	}
	return targets
}
