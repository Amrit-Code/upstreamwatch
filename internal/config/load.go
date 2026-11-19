package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"regexp"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// Load reads the YAML file at path, applies defaults, expands ${NAME}
// references in string values with getenv and validates the result. Unknown
// keys are an error. Secret fields accept only an empty value or a single
// ${NAME} reference. Every problem found is returned, joined; on any error
// the returned Config is the zero value.
func Load(path string, getenv func(string) string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	f := defaultFile()
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	// io.EOF means the file holds no document; the defaults then stand.
	err = dec.Decode(&f)
	switch {
	case errors.Is(err, io.EOF):
	case err != nil:
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	default:
		if err := onlyEmptyDocuments(dec); err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", path, err)
		}
	}

	cfg, errs := convert(f, getenv)
	errs = append(errs, cfg.validate()...)
	for i, ch := range f.Notify.Channels {
		errs = append(errs, validateChannel(fmt.Sprintf("notify.channels[%d]", i), ch, cfg.Notify.Channels[i])...)
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// errMultiDoc is returned when the config file holds a second document with
// content.
var errMultiDoc = errors.New("more than one YAML document")

// onlyEmptyDocuments reads the documents after the first until io.EOF. Each
// is decoded only to see whether it has content; an empty or null document (a
// bare trailing ---, one holding only a comment, ~ or null) is ignored. Any
// other document, or a decode error, is errMultiDoc.
func onlyEmptyDocuments(dec *yaml.Decoder) error {
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil || !emptyDocument(&doc) {
			return errMultiDoc
		}
	}
}

// emptyDocument reports whether n is a document with no content or whose
// only content is a null scalar.
func emptyDocument(n *yaml.Node) bool {
	if n.Kind != yaml.DocumentNode {
		return false
	}
	switch len(n.Content) {
	case 0:
		return true
	case 1:
		c := n.Content[0]
		return c.Kind == yaml.ScalarNode && c.Tag == "!!null"
	}
	return false
}

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expand replaces ${NAME} references in a decoded string value. Unset names
// become empty; validation reports required fields that end up empty.
func expand(s string, getenv func(string) string) string {
	return envRef.ReplaceAllStringFunc(s, func(m string) string {
		return getenv(m[2 : len(m)-1])
	})
}

// expandAll expands every element of a decoded list, keeping nil as nil.
func expandAll(list []string, getenv func(string) string) []string {
	if list == nil {
		return nil
	}
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = expand(s, getenv)
	}
	return out
}

// secret accepts only a value that is exactly one ${NAME} reference, because
// secrets stay out of the YAML file. The error never repeats the value.
func secret(field, raw string, getenv func(string) string) (model.Secret, error) {
	if raw == "" {
		return "", nil
	}
	m := envRef.FindStringSubmatch(raw)
	if m == nil || m[0] != raw {
		return "", fmt.Errorf("%s: must be an environment reference such as ${UPW_GITHUB_TOKEN_UPSTREAM}, not a literal", field)
	}
	return model.Secret(getenv(m[1])), nil
}

// convert builds the typed Config from the decoded file, expanding string
// values, resolving secrets and parsing durations. It returns one error per
// literal secret and per duration that does not parse or is not positive.
func convert(f fileConfig, getenv func(string) string) (Config, []error) {
	var errs []error
	sec := func(field, raw string) model.Secret {
		s, err := secret(field, raw, getenv)
		if err != nil {
			errs = append(errs, err)
		}
		return s
	}
	ex := func(s string) string { return expand(s, getenv) }
	dur := func(field, raw string) time.Duration {
		d, err := positiveDuration(field, ex(raw))
		if err != nil {
			errs = append(errs, err)
		}
		return d
	}

	cfg := Config{
		DataDir: ex(f.DataDir),
		HTTP:    HTTP{Listen: ex(f.HTTP.Listen), BaseURL: ex(f.HTTP.BaseURL)},
		Auth: Auth{
			GitHubTargetsToken:  sec("auth.github_targets_token", f.Auth.GitHubTargetsToken),
			GitHubUpstreamToken: sec("auth.github_upstream_token", f.Auth.GitHubUpstreamToken),
		},
		Schedules: Schedules{
			Ingest: ex(f.Schedules.Ingest),
			Vulns:  ex(f.Schedules.Vulns),
			Issues: ex(f.Schedules.Issues),
			LLM:    ex(f.Schedules.LLM),
			Digest: ex(f.Schedules.Digest),
			Backup: ex(f.Schedules.Backup),
		},
		Vulns: Vulns{
			EcosystemsExtra:           expandAll(f.Vulns.EcosystemsExtra, getenv),
			ExposureBasis:             ex(f.Vulns.ExposureBasis),
			DeriveToolchainFromImages: f.Vulns.DeriveToolchainFromImages,
		},
		Upstream: Upstream{
			BackfillDays:      f.Upstream.BackfillDays,
			BackfillMaxIssues: f.Upstream.BackfillMaxIssues,
			PollPullRequests:  f.Upstream.PollPullRequests,
		},
		Prefilter: Prefilter{
			Threshold:         f.Prefilter.Threshold,
			IncludeHistorical: f.Prefilter.IncludeHistorical,
			Weights:           maps.Clone(f.Prefilter.Weights),
		},
		LLM: LLM{
			Enabled:          f.LLM.Enabled,
			Provider:         ex(f.LLM.Provider),
			Model:            ex(f.LLM.Model),
			APIKey:           sec("llm.api_key", f.LLM.APIKey),
			BaseURL:          ex(f.LLM.BaseURL),
			Effort:           ex(f.LLM.Effort),
			SnippetMode:      ex(f.LLM.SnippetMode),
			MaxSnippets:      f.LLM.MaxSnippets,
			MaxIssueChars:    f.LLM.MaxIssueChars,
			MaxCallsPerRun:   f.LLM.MaxCallsPerRun,
			MaxUSDPerDay:     f.LLM.MaxUSDPerDay,
			PricesPerMTok:    Prices{Input: f.LLM.PricesPerMTok.Input, Output: f.LLM.PricesPerMTok.Output},
			RejudgeOnComment: f.LLM.RejudgeOnComment,
		},
		Notify: Notify{Heartbeat: ex(f.Notify.Heartbeat)},
		Retention: Retention{
			BackupsKeep:          f.Retention.BackupsKeep,
			IssuesClosedKeepDays: f.Retention.IssuesClosedKeepDays,
		},
		Git: Git{
			MaxCloneMB:   f.Git.MaxCloneMB,
			CloneTimeout: dur("git.clone_timeout", f.Git.CloneTimeout),
			FetchTimeout: dur("git.fetch_timeout", f.Git.FetchTimeout),
		},
	}

	for _, t := range f.Targets {
		auth := TargetAuth(ex(t.Auth))
		if auth == "" {
			auth = "none"
		}
		cfg.Targets = append(cfg.Targets, Target{
			Slug:              ex(t.Slug),
			URL:               ex(t.URL),
			Branch:            ex(t.Branch),
			Auth:              auth,
			Ecosystems:        expandAll(t.Ecosystems, getenv),
			Unity:             t.Unity,
			AllowPrivateHosts: t.AllowPrivateHosts,
			ExcludePaths:      expandAll(t.ExcludePaths, getenv),
		})
	}

	if f.Upstream.Overrides != nil {
		cfg.Upstream.Overrides = make(map[string]UpstreamOverride, len(f.Upstream.Overrides))
		for purl, o := range f.Upstream.Overrides {
			watch := true
			if o.WatchIssues != nil {
				watch = *o.WatchIssues
			}
			cfg.Upstream.Overrides[purl] = UpstreamOverride{Repo: ex(o.Repo), WatchIssues: watch}
		}
	}

	for i, ch := range f.Notify.Channels {
		path := fmt.Sprintf("notify.channels[%d]", i)
		typ := ex(ch.Type)
		var port int
		switch {
		case ch.Port != nil:
			port = *ch.Port
		case typ == "email":
			port = defaultEmailPort
		}
		cfg.Notify.Channels = append(cfg.Notify.Channels, Channel{
			Type:     typ,
			URL:      sec(path+".url", ch.URL),
			Host:     ex(ch.Host),
			Port:     port,
			Username: ex(ch.Username),
			Password: sec(path+".password", ch.Password),
			From:     ex(ch.From),
			To:       expandAll(ch.To, getenv),
		})
	}

	return cfg, errs
}
