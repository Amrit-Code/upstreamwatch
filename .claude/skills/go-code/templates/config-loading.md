# Scaffold: config loading with ${ENV} references and redacted secrets

Use this when writing or extending `internal/config`. The field list comes from `mvp-plan/PLAN.md` section 7 and the email notifier settings in `docs/external-sources.md`. The YAML library is `go.yaml.in/yaml/v3` (approved 2026-11-19). Decode with a `yaml.Decoder` and `KnownFields(true)`, so that a typo in `upw.yaml` is an error instead of a silently ignored setting. `internal/config` is the working example.

## Two structs: what the file says, and what the program uses

`fileConfig` mirrors the YAML with plain strings. `Config` holds expanded, validated, typed values. Only `Config` leaves the package.

`Secret` is declared in `internal/model`, not here, because adapters and the LLM `KeySource` (`docs/interfaces.md`) use it too and must not import `config`. It is shown below so the redaction rules are in one place; in `internal/config` refer to it as `model.Secret`.

```go
// Secret is a credential read from the environment. It prints, logs and
// marshals as its last four characters so it cannot leak through fmt, slog or JSON.
type Secret string

// String returns the redacted form.
func (s Secret) String() string { return redact(string(s)) }

// GoString keeps %#v redacted.
func (s Secret) GoString() string { return redact(string(s)) }

// LogValue keeps slog output redacted.
func (s Secret) LogValue() slog.Value { return slog.StringValue(redact(string(s))) }

// MarshalText keeps JSON and text encodings redacted.
func (s Secret) MarshalText() ([]byte, error) { return []byte(redact(string(s))), nil }

// Reveal returns the value. Call it only where the credential is sent:
// an Authorization header, SMTP auth, the provider client.
func (s Secret) Reveal() string { return string(s) }

func redact(v string) string {
	switch {
	case v == "":
		return ""
	case len(v) <= 8:
		return "****"
	default:
		return "****" + v[len(v)-4:]
	}
}
```

## Expansion: only `${NAME}`, only in decoded values

```go
var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expand replaces ${NAME} references in a decoded string value. Unset names
// become empty; validation reports required fields that end up empty.
func expand(s string, getenv func(string) string) string {
	return envRef.ReplaceAllStringFunc(s, func(m string) string {
		return getenv(m[2 : len(m)-1])
	})
}

// secret accepts only a value that is exactly one ${NAME} reference, because
// secrets stay out of the YAML file.
func secret(field, raw string, getenv func(string) string) (Secret, error) {
	if raw == "" {
		return "", nil
	}
	m := envRef.FindStringSubmatch(raw)
	if m == nil || m[0] != raw {
		return "", fmt.Errorf("%s: must be an environment reference such as ${UPW_GITHUB_TOKEN_UPSTREAM}, not a literal", field)
	}
	return Secret(getenv(m[1])), nil
}
```

## Load: defaults, decode, expand, validate, report everything

```go
// Load reads the YAML file at path, expands ${NAME} references with getenv,
// and validates the result. It returns every problem found, joined.
func Load(path string, getenv func(string) string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	f := defaultFile() // defaults live in code
	if err := yaml.Unmarshal(b, &f); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}

	var errs []error
	cfg := Config{
		DataDir: expand(f.DataDir, getenv),
		HTTP:    HTTP{Listen: expand(f.HTTP.Listen, getenv), BaseURL: expand(f.HTTP.BaseURL, getenv)},
	}

	var e error
	cfg.Auth.GitHubTargetsToken, e = secret("auth.github_targets_token", f.Auth.GitHubTargetsToken, getenv)
	errs = append(errs, e)
	cfg.Auth.GitHubUpstreamToken, e = secret("auth.github_upstream_token", f.Auth.GitHubUpstreamToken, getenv)
	errs = append(errs, e)

	errs = append(errs, cfg.validate()...)
	if err := errors.Join(errs...); err != nil { // Join drops nil entries
		return Config{}, err
	}
	return cfg, nil
}

// validate returns one error per problem so the operator can fix them in one pass.
func (c Config) validate() []error {
	var errs []error
	if c.DataDir == "" {
		errs = append(errs, errors.New("data_dir: required"))
	}
	if c.HTTP.Listen == "" {
		errs = append(errs, errors.New("http.listen: required"))
	}
	for i, t := range c.Targets {
		if t.Slug == "" {
			errs = append(errs, fmt.Errorf("targets[%d].slug: required", i))
		}
	}
	return errs
}
```

The URL rules for targets (https only, private-address rejection) live where `outbound-client` puts them. They are checked again before every fetch, so config validation alone is not the guard.
