package main

import (
	"log/slog"
	"path/filepath"

	"github.com/Amrit-Code/upstreamwatch/internal/config"
	"github.com/Amrit-Code/upstreamwatch/internal/extract"
	"github.com/Amrit-Code/upstreamwatch/internal/extract/unity"
	"github.com/Amrit-Code/upstreamwatch/internal/gitsrc"
	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/ingest"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/ops"
	"github.com/Amrit-Code/upstreamwatch/internal/pipeline"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
	"github.com/Amrit-Code/upstreamwatch/internal/timeline"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln/epss"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln/kev"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln/osv"
)

// registeredJobs returns every job the Runner knows, in pipeline order:
// ingest, timeline, vulns, backup. Ingest and timeline share one git client;
// the HTTP adapters share one httpx client that keeps its validators in the
// store. Each job runs under its lock file in <data_dir>/locks, so two
// processes on one data directory never run the same job at once.
// Constructing them does no I/O.
func registeredJobs(cfg config.Config, db *store.DB, logger *slog.Logger) []pipeline.Job {
	git := gitsrc.New(gitOptions(cfg), logger)
	hc := httpx.New(db, httpx.Options{}, logger)
	extractors := []timeline.Extractor{
		extract.New(extract.Options{DeriveToolchainFromImages: cfg.Vulns.DeriveToolchainFromImages}, logger),
	}
	return withJobLocks(filepath.Join(cfg.DataDir, "locks"), []pipeline.Job{
		ingest.NewService(git, db, ingest.Options{Targets: ingestTargets(cfg)}, logger),
		timeline.NewService(git, db, db, extractors, unity.New(), timeline.Options{Targets: timelineTargets(cfg)}, logger),
		vuln.NewService(
			osv.New(hc, osv.Options{}, logger),
			kev.New(hc, kev.Options{}, logger),
			epss.New(hc, epss.Options{}, logger),
			db, db, db,
			vuln.Options{ExposureBasis: cfg.Vulns.ExposureBasis, EcosystemsExtra: cfg.Vulns.EcosystemsExtra},
			logger),
		ops.NewBackup(db, ops.BackupOptions{Dir: filepath.Join(cfg.DataDir, "backups"), Keep: cfg.Retention.BackupsKeep}, logger),
	})
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
