---
name: web-ui
description: How to add or change a page, form or handler in upstreamwatch's server-rendered web UI in internal/web - html/template parsed from an embedded FS at startup, the shared layout, thin handlers that parse input, call a service and render, POST-redirect-GET forms, the same-origin check on every POST, escaping rules for upstream text and model output, the write-only settings page, and golden-file tests of rendered pages. Triggers on "add a page", "new route", "template", "handler", "form", "ack button", "snooze", "label buttons", "settings page", "findings page", "coverage page", "timeline page", "/f/", "style.css", or any edit under internal/web. Not for visual design quality (use the impeccable:impeccable plugin skill if installed), the services a handler calls (use upw-architecture and go-code), or how the LLM key is stored (use llm-triage).
---

# Web UI

The UI is a handful of server-rendered pages for one operator on the tailnet. It has no login: it is not reachable from outside the host except through `tailscale serve`, so Tailscale identity is the auth boundary. (Run directly, the binary listens on the config default `127.0.0.1:8080`. In the container the config sets `0.0.0.0:8080` and compose publishes it as `127.0.0.1:8080:8080`, host loopback only.) It renders text that strangers wrote (upstream issues) and text a model wrote (verdicts), which is why the escaping rules below are strict.

- The routes table (method, path, what handles it) lives in `docs/architecture.md`. Add or change a route there in the same change (`docs-sync`).
- What each page shows: PLAN.md section 6, "Web UI".

## Shape of the package

- Templates in `internal/web/templates/*.html` and `internal/web/static/style.css`, embedded with `embed.FS`.
- Parse everything once at startup and fail startup on a parse error, so a broken template never reaches a request. Parse the layout and shared partials into a base set, then for each page `Clone()` the base and parse that page's file into the clone. Pages that each `define "content"` collide if parsed into one set.
- Execute into a `bytes.Buffer`, then write it. A template error halfway through then becomes a clean 500 instead of half a page with a 200 status.
- One small `FuncMap`, registered once, for formatting (dates, money, relative times). No logic that decides anything goes in templates or the FuncMap.

## Handlers

A handler does three things: parse input, call one service, render.

- Routes use `net/http` method patterns (`"GET /f/{id}"`, `"POST /f/{id}/ack"`) and `r.PathValue`. No router library.
- No SQL, no `store` import, no domain decisions in `internal/web`. Whether a transition is legal is the domain object's call, so the CLI and the digest get the same answer. The layering lint in `make check` enforces the imports.
- GET handlers call the read services; POST handlers call `findings.Service`, `eval.Service` or the settings service. The web server writes only finding state, eval labels and settings.
- Pass each template a page-specific view struct built from what the service returned. Do not hand templates domain objects to call methods on.
- Unknown ID: 404. Bad input: 400 with a short message. Anything else: 500 with a generic message, the detail in the log only.

## Forms and POST

- State changes only on POST. A GET never changes anything.
- POST-redirect-GET: on success, respond `303 See Other` to the page that shows the result (`/f/{id}` after ack). The page displaying the new state is the confirmation; no flash-message machinery. On a validation error, re-render the form with the message and status 400.
- Wrap the mux with `http.NewCrossOriginProtection()` (Go 1.25 and later). It rejects cross-origin non-safe requests using `Sec-Fetch-Site`, falling back to comparing `Origin` with `Host`. If the fallback rejects legitimate posts behind `tailscale serve`, add the configured `http.base_url` with `AddTrustedOrigin`; do not disable the check. No CORS headers anywhere.
- Limit request bodies with `http.MaxBytesReader` before `r.ParseForm`. These forms are a few fields; a small limit suffices.

## Escaping: upstream text and model output

- Issue titles, bodies and comments, advisory summaries and details, and every field the model wrote (`reasoning`, `evidence`, `justification`, `fixed_in`) render through ordinary `{{.Field}}` actions inside `<pre>` (or an element styled `white-space: pre-wrap`).
- Never convert any of it to `template.HTML`, `template.JS`, `template.URL`, `template.CSS` or `template.HTMLAttr`. Those types switch off escaping. No markdown rendering of upstream or model text.
- Links: put only URLs we built (the issue URL from owner, repo and number) or validated `https` URLs in `href`, with `rel="noopener noreferrer"`. `html/template` already neutralises `javascript:` URLs; do not rely on that alone.
- The "what was sent to the model" view on `/f/{id}` shows the stored request payload in `<pre>` as plain text. It may contain private code in `snippets` mode, which is why it stays on the tailnet.

## No JavaScript

- No JavaScript build, no client framework, no inline scripts, no CDN assets. Everything works as plain HTML forms and links. A filter box is a GET form.
- Because there is no script, send a strict policy on every response from one middleware: `Content-Security-Policy: default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; frame-ancestors 'none'` and `X-Content-Type-Options: nosniff`.

## The settings page

`GET/POST /settings` takes the LLM API key (M4). The key is write-only:

- The input is `type="password"` with `autocomplete="off"` and is never pre-filled. The page shows only the last four characters of the stored key.
- When `UPW_LLM_API_KEY` is set, the page says the env var is in effect and the stored key is ignored.
- The submitted value is never echoed in a redirect URL, a message, a log line or an error.
- The handler passes the value to the service that stores it; how it is encrypted and resolved is in `llm-triage`.

## Tests

- Golden-file tests of each page: render with fixed view data and a fixed clock, compare with `testdata/<name>.golden` (for example `testdata/finding_detail.golden`), regenerate with the package's `-update` flag. Golden mechanics are in `go-test`.
- Every page that shows upstream or model text has a golden case where that text contains `<script>alert(1)</script>`, `</pre>` and a `javascript:` link, proving they render escaped.
- Handler tests with `httptest`: a POST with `Sec-Fetch-Site: cross-site` gets 403; a successful POST returns 303 to the right page; a GET never calls a mutating service method (the fake service records calls).
- `/health` returns JSON, not a template; test its shape.

## Visual design

For the look of a page (layout, hierarchy, typography, spacing, the stylesheet), use the `impeccable:impeccable` plugin skill, if it is installed in the session, when building a new page or restyling an existing one. Its output must stay within this skill's constraints: one stylesheet, no inline `style` attributes (the CSP blocks them), no JavaScript, no web fonts from third-party hosts, no framework. Skip it for handler-only or wiring changes.

## Not covered here

- The services handlers call, and Go style: `upw-architecture`, `go-code`.
- Golden-file and httptest mechanics: `go-test`.
- How the LLM key is stored and resolved: `llm-triage`.
- Updating the routes table in `docs/architecture.md`: `docs-sync`. Reviewing write endpoints: `security-check`.
