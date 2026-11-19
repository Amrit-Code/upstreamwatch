---
name: security-check
description: Read-only security review of an upstreamwatch diff or a whole milestone, producing a ranked report in which each finding has file and line, the path from input to sink, a one-sentence exploit and a fix, ending in a verdict. Checks this project's threat list - token scopes and logging, untrusted upstream issue text, nothing from a target or upstream executed, arbitrary git URLs and SSRF, the outbound surface, secrets in clones, UI write endpoints, supply chain, prompt injection and the stored LLM key. Triggers on "security review", "security check", "vuln check", "is this safe", "milestone security review", "run govulncheck", or a change touching internal/gitsrc, internal/httpx, internal/llm, internal/web POST handlers, settings, tokens or go.mod. Not for correctness review against a work order (use review-gate), writing fixes (findings go back to a builder via work-order), or dependency approval (the owner decides).
---

# Security check

A vulnerability review that reports and changes nothing. The reviewer writes no code, no tests, no files and no commits; its only output is the report, returned as its final message. Fixes go back to a builder through a new work order. Run it in a fresh context, never in the session that wrote or briefed the code, because the separation is the point.

## Scope

- **Diff review:** the change named in the request, usually the working tree against the last commit (`git diff HEAD` plus untracked files) or a commit range the lead gives you. Read enough surrounding code to judge each change.
- **Milestone review:** the commit range for the milestone, given by the lead. Sweep the whole threat list below, not only the changed lines, because a milestone is when new surface goes live.

## Run the tools first

```bash
govulncheck ./...      # known vulnerabilities reachable from our code
go vet ./...
git diff <base> -- go.mod go.sum   # any new or changed module
```

If a tool is missing, say so in the report and continue. Then search for the usual sinks and trace each hit:

```bash
grep -rn --include='*.go' -e 'exec.Command' -e '"sh"' -e 'template.HTML' -e 'template.JS' -e 'template.URL' \
  -e 'InsecureSkipVerify' -e 'http.Get(' -e 'http.Post(' -e 'http.DefaultClient' -e '&http.Client{' \
  -e 'Sprintf("SELECT' -e 'Sprintf("INSERT' -e 'Sprintf("UPDATE' -e 'Sprintf("DELETE' -e 'os.Environ()' .
```

## How to decide what is a finding

- List the entry points of untrusted data first: config (`upw.yaml`, env), HTTP requests to the UI, responses from every outside service (GitHub, OSV, deps.dev, ecosyste.ms, go-get meta pages, KEV, EPSS), the contents of target repos (manifests, source at HEAD, commit subjects, paths, branch names), and model output.
- Trace each suspicious sink back to an entry point. A sink fed only by constants or internal values is not a finding.
- A guard upstream of the sink (a parser, an allowlist, a type) lowers severity; it does not delete the finding.
- No plausible exploit scenario, no finding. Zero findings is a valid result; do not pad.

## Threat list

Check every item on a milestone review, and the items the diff touches on a diff review. Facts about services and interfaces are in `docs/external-sources.md`, `docs/interfaces.md` and `docs/data-access.md`; the rules being checked are in the `outbound-client`, `llm-triage` and `web-ui` skills.

1. **Token scopes and logging.** The targets token is sent only to target remotes with `auth: targets`; the upstream token only to `api.github.com`. No token, key, SMTP password or webhook URL appears in a log line, error string, URL, argv, stored row (`http_cache`, `request_payload`, `digests`), export or rendered page. `upw doctor` shows the last four characters only.
2. **Untrusted upstream text.** Issue and comment text is stored as text, read through size caps, truncated before prompting, rendered escaped inside `<pre>`, and markdown-neutralised in the digest (no live mentions such as `@everyone`, no injected links). No `template.HTML` or similar on it.
3. **Nothing from a target or upstream is executed.** No package manager, `go list`, `pip`, build tool, hook, `git submodule`, checkout or working tree. Manifests are parsed as bytes. Downloaded archives (the EPSS gzip) are read with a cap on decompressed size.
4. **Arbitrary git URLs and SSRF.** `https` only, no userinfo, private, loopback, link-local, multicast and `100.64.0.0/10` addresses rejected after DNS resolution: at the dialer for HTTP, before every git network operation for git, with the checked address pinned. Redirects re-checked (httpx) or disabled (git). `protocol.allow=never` except https, `--` and `--end-of-options` before user values, timeouts, the clone size cap, no submodules. URLs that arrive from deps.dev, ecosyste.ms and go-get meta tags count as untrusted input too.
5. **Outbound surface.** Every HTTP call goes through `internal/httpx`; a direct `http.Get` or new `http.Client` bypasses the URL policy. Any host not listed in `docs/external-sources.md` is a finding.
6. **Secrets in clones.** Bare clones live under the data dir with mode 700, are never served by the UI, and never reach the model: snippets come from source at HEAD only, and `snippets` mode is opt-in. The git subprocess gets an explicit minimal environment, not the process's.
7. **UI write endpoints.** State changes on POST only, every POST behind the same-origin check, request bodies size-limited, no CORS headers, the service not reachable from outside the host (the config default is `127.0.0.1:8080` when run directly; in the container the config sets `0.0.0.0:8080` and compose publishes `127.0.0.1:8080:8080`, so only `tailscale serve` reaches it), no state change on GET.
8. **Supply chain.** `go.sum` committed; any new module is on the approved list in PLAN.md or has the owner's recorded yes, with the import path checked for typosquats; `govulncheck` clean or each hit explained; base images and the deployed image pinned by digest.
9. **Prompt injection.** The model has no tools; issue text is wrapped with its delimiters neutralised so it cannot close the data block; output is schema-validated after decoding; evidence quotes are checked as substrings; no code path lets a verdict change finding state; model output is escaped in the UI and neutralised in the digest; the default `snippet_mode` is `symbols`.
10. **The stored LLM key.** The provider adapters read the key through a `KeySource` on every call; `cmd/upw` wires it to `UPW_LLM_API_KEY` if set (it takes precedence), otherwise to `settings.Service`, the only code that encrypts and decrypts, and `triage` never reads the key. The stored key is AES-GCM encrypted with a random nonce, its key file is created mode 600 in the data dir; the settings page never renders the key back (last four only) and never echoes a submitted value; the key is absent from logs, errors, exports and request payloads.

Also report SQL built by string formatting from any input, and any unbounded read of a response or request body.

## Severity

| Level | Meaning here |
|---|---|
| critical | Exploitable by whoever controls upstream issue text, a target repo's contents, or a URL the tool will fetch, giving code execution, token or key disclosure, or a connection to an internal or tailnet host. |
| high | The same outcomes, but needing a second failure or a value only the operator controls, or a verdict able to change finding state. |
| medium | A missing defence with limited reach: an unbounded read, a missing size cap, a token shown in full to the operator. |
| low | Hardening: headers, tighter timeouts, defence in depth. |

## Report format

Rank findings by severity, highest first. For each:

```text
[high] Upstream URL from go-get meta skips the private-address check
internal/upstream/goget.go:88
Path: target go.mod module path -> GoGetMeta.Resolve -> repo URL from meta tag -> issues adapter -> httpx client built with allow-private
Exploit: a vanity module whose meta tag points at https://100.101.102.103/ makes the poller fetch from a tailnet host.
Fix: build the resolver's client with the production policy and re-validate the returned URL as https.
```

After the findings:

- **Tools:** one line each for `govulncheck`, `go vet` and the `go.mod` diff, with what they reported.
- **Checked, nothing found:** the threat-list items you covered with no finding, one line each, so the reader knows what the review covered.
- **Verdict:** either "clear" or "do not accept: N findings at high or above". The lead and the owner decide what happens next.

## Not covered here

- Correctness review of a change against its work order: `review-gate`.
- Turning findings into fixes: `work-order`, then a builder.
- The rules this review checks, in detail: `outbound-client`, `llm-triage`, `web-ui`. Milestone acceptance commands: `milestone-accept`.
