package config

// fileConfig mirrors upw.yaml. String values are kept as written, before
// ${NAME} expansion; secret fields hold the raw reference.
type fileConfig struct {
	DataDir   string        `yaml:"data_dir"`
	HTTP      fileHTTP      `yaml:"http"`
	Auth      fileAuth      `yaml:"auth"`
	Targets   []fileTarget  `yaml:"targets"`
	Schedules fileSchedules `yaml:"schedules"`
	Vulns     fileVulns     `yaml:"vulns"`
	Upstream  fileUpstream  `yaml:"upstream"`
	Prefilter filePrefilter `yaml:"prefilter"`
	LLM       fileLLM       `yaml:"llm"`
	Notify    fileNotify    `yaml:"notify"`
	Retention fileRetention `yaml:"retention"`
	Git       fileGit       `yaml:"git"`
}

type fileHTTP struct {
	Listen  string `yaml:"listen"`
	BaseURL string `yaml:"base_url"`
}

type fileAuth struct {
	GitHubTargetsToken  string `yaml:"github_targets_token"`
	GitHubUpstreamToken string `yaml:"github_upstream_token"`
}

type fileTarget struct {
	Slug       string   `yaml:"slug"`
	URL        string   `yaml:"url"`
	Branch     string   `yaml:"branch"`
	Auth       string   `yaml:"auth"`
	Ecosystems []string `yaml:"ecosystems"`
	Unity      bool     `yaml:"unity"`
	// AllowPrivateHosts exempts the target's host from the private-address
	// check.
	AllowPrivateHosts bool `yaml:"allow_private_hosts"`
}

type fileSchedules struct {
	Ingest string `yaml:"ingest"`
	Vulns  string `yaml:"vulns"`
	Issues string `yaml:"issues"`
	LLM    string `yaml:"llm"`
	Digest string `yaml:"digest"`
	Backup string `yaml:"backup"`
}

type fileVulns struct {
	EcosystemsExtra           []string `yaml:"ecosystems_extra"`
	ExposureBasis             string   `yaml:"exposure_basis"`
	DeriveToolchainFromImages bool     `yaml:"derive_toolchain_from_images"`
}

type fileUpstream struct {
	BackfillDays      int                     `yaml:"backfill_days"`
	BackfillMaxIssues int                     `yaml:"backfill_max_issues"`
	PollPullRequests  bool                    `yaml:"poll_pull_requests"`
	Overrides         map[string]fileOverride `yaml:"overrides"`
}

type fileOverride struct {
	Repo string `yaml:"repo"`
	// WatchIssues is a pointer so an override that only sets repo keeps the
	// default of watching, matching upstream_repos.watch_issues DEFAULT 1.
	WatchIssues *bool `yaml:"watch_issues"`
}

type filePrefilter struct {
	Threshold         int            `yaml:"threshold"`
	IncludeHistorical bool           `yaml:"include_historical"`
	Weights           map[string]int `yaml:"weights"`
}

type fileLLM struct {
	Enabled          bool       `yaml:"enabled"`
	Provider         string     `yaml:"provider"`
	Model            string     `yaml:"model"`
	APIKey           string     `yaml:"api_key"`
	BaseURL          string     `yaml:"base_url"`
	Effort           string     `yaml:"effort"`
	SnippetMode      string     `yaml:"snippet_mode"`
	MaxSnippets      int        `yaml:"max_snippets"`
	MaxIssueChars    int        `yaml:"max_issue_chars"`
	MaxCallsPerRun   int        `yaml:"max_calls_per_run"`
	MaxUSDPerDay     float64    `yaml:"max_usd_per_day"`
	PricesPerMTok    filePrices `yaml:"prices_per_mtok"`
	RejudgeOnComment bool       `yaml:"rejudge_on_comment"`
}

type filePrices struct {
	Input  float64 `yaml:"input"`
	Output float64 `yaml:"output"`
}

type fileNotify struct {
	Channels  []fileChannel `yaml:"channels"`
	Heartbeat string        `yaml:"heartbeat"`
}

type fileChannel struct {
	Type string `yaml:"type"`
	URL  string `yaml:"url"`
	Host string `yaml:"host"`
	// Port is a pointer so an absent port takes the default while an
	// explicit 0 is reported.
	Port     *int     `yaml:"port"`
	Username string   `yaml:"username"`
	Password string   `yaml:"password"`
	From     string   `yaml:"from"`
	To       []string `yaml:"to"`
}

type fileRetention struct {
	BackupsKeep          int `yaml:"backups_keep"`
	IssuesClosedKeepDays int `yaml:"issues_closed_keep_days"`
}

// fileGit keeps the timeouts as written, Go duration strings, so a bad value
// is reported with its field like every other validation problem.
type fileGit struct {
	MaxCloneMB   int    `yaml:"max_clone_mb"`
	CloneTimeout string `yaml:"clone_timeout"`
	FetchTimeout string `yaml:"fetch_timeout"`
}

// defaultEmailPort is the SMTP submission port used when an email channel
// sets none.
const defaultEmailPort = 587

// defaultFile returns the defaults; the YAML is decoded over them.
func defaultFile() fileConfig {
	return fileConfig{
		DataDir: "./data",
		HTTP:    fileHTTP{Listen: "127.0.0.1:8080"},
		Schedules: fileSchedules{
			Ingest: "5 */6 * * *",
			Vulns:  "10 */6 * * *",
			Issues: "15 * * * *",
			LLM:    "20 * * * *",
			Digest: "0 8 * * *",
			Backup: "30 3 * * *",
		},
		Vulns: fileVulns{
			ExposureBasis:             "published",
			DeriveToolchainFromImages: true,
		},
		Upstream: fileUpstream{
			BackfillDays:      365,
			BackfillMaxIssues: 2000,
			PollPullRequests:  true,
		},
		Prefilter: filePrefilter{Threshold: 3},
		LLM: fileLLM{
			Enabled:        false,
			Provider:       "anthropic",
			Model:          "claude-opus-5-5",
			Effort:         "low",
			SnippetMode:    "symbols",
			MaxSnippets:    5,
			MaxIssueChars:  12000,
			MaxCallsPerRun: 50,
			MaxUSDPerDay:   2.00,
			PricesPerMTok:  filePrices{Input: 4.00, Output: 20.00},
		},
		Notify: fileNotify{Heartbeat: "0 8 * * 1"},
		Retention: fileRetention{
			BackupsKeep:          7,
			IssuesClosedKeepDays: 730,
		},
		Git: fileGit{
			MaxCloneMB:   2048,
			CloneTimeout: "30m",
			FetchTimeout: "10m",
		},
	}
}
