package config

import (
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// Config is the loaded, expanded and validated configuration. Load returns it;
// cmd/upw copies the parts each service needs into that service's Options.
type Config struct {
	// DataDir holds the SQLite database, the bare clones and the backups.
	DataDir   string
	HTTP      HTTP
	Auth      Auth
	Targets   []Target
	Schedules Schedules
	Vulns     Vulns
	Upstream  Upstream
	Prefilter Prefilter
	LLM       LLM
	Notify    Notify
	Retention Retention
	Git       Git
}

// HTTP is the web UI listener. BaseURL, when set, is used in digest links.
type HTTP struct{ Listen, BaseURL string }

// Auth holds the two GitHub tokens: one for the private target repositories
// and one, public read-only, for upstream issue trackers.
type Auth struct{ GitHubTargetsToken, GitHubUpstreamToken model.Secret }

// Target is one repository whose dependency history is mined. Branch is empty
// for the default branch; Ecosystems, when not empty, is an allowlist; Unity
// enables the UPM extractor.
type Target struct {
	Slug, URL, Branch string
	Auth              TargetAuth // "targets" | "upstream" | "none"
	Ecosystems        []string
	Unity             bool
	AllowPrivateHosts bool // exempts this target's host from the private-address check
}

// TargetAuth names the token used to fetch a target: "targets", "upstream"
// or "none".
type TargetAuth string

// Schedules holds one five-field cron expression per scheduled job.
type Schedules struct{ Ingest, Vulns, Issues, LLM, Digest, Backup string } // cron expressions

// Vulns configures advisory matching. ExposureBasis is "published" or "pin".
type Vulns struct {
	// EcosystemsExtra adds OSV ecosystems to pull modified_id.csv for.
	EcosystemsExtra []string
	ExposureBasis   string
	// DeriveToolchainFromImages maps base images such as golang:1.26-alpine
	// to a toolchain package.
	DeriveToolchainFromImages bool
}

// Upstream configures upstream resolution and the issue backfill.
type Upstream struct {
	BackfillDays      int
	BackfillMaxIssues int
	PollPullRequests  bool
	// Overrides is keyed by package purl.
	Overrides map[string]UpstreamOverride
}

// UpstreamOverride maps one package to an upstream repository and says
// whether that repository's issues are watched. WatchIssues defaults to true.
type UpstreamOverride struct {
	Repo        string
	WatchIssues bool
}

// Prefilter configures the deterministic issue prefilter. Weights overrides
// prefilter feature weights by feature name.
type Prefilter struct {
	Threshold         int
	IncludeHistorical bool
	Weights           map[string]int
}

// LLM configures the triage stage. Provider is "anthropic" or
// "openai_compat"; SnippetMode is "symbols", "snippets" or "none". BaseURL is
// used by openai_compat only and Effort by anthropic only.
type LLM struct {
	Enabled          bool
	Provider         string
	Model            string
	APIKey           model.Secret
	BaseURL          string
	Effort           string
	SnippetMode      string
	MaxSnippets      int
	MaxIssueChars    int
	MaxCallsPerRun   int
	MaxUSDPerDay     float64
	PricesPerMTok    Prices
	RejudgeOnComment bool
}

// Prices are US dollars per million tokens, used for the daily cost cap only.
type Prices struct{ Input, Output float64 }

// Notify lists the digest channels and the heartbeat schedule.
type Notify struct {
	Channels []Channel
	// Heartbeat is the cron expression for the "all quiet" message.
	Heartbeat string
}

// Channel is one notifier. Type is "email", "discord_webhook",
// "slack_webhook", "ntfy" or "stdout". URL is set for the webhook and ntfy
// types; Host, Port, Username, Password, From and To for email.
type Channel struct {
	Type     string
	URL      model.Secret
	Host     string
	Port     int
	Username string
	Password model.Secret
	From     string
	To       []string
}

// Retention says how much history is kept.
type Retention struct {
	BackupsKeep          int
	IssuesClosedKeepDays int
}

// Git limits the bare clones of the targets: MaxCloneMB caps one clone's size
// in MiB, CloneTimeout bounds a first sync and FetchTimeout every later one.
type Git struct {
	MaxCloneMB   int
	CloneTimeout time.Duration
	FetchTimeout time.Duration
}
