---
name: llm-triage
description: How to work on upstreamwatch's LLM stage - internal/llm (the pure helpers: provider-neutral prompt builder, vocabulary constants, output schema, cost helpers; plus the anthropic and openaicompat provider adapters) and internal/triage (the service that turns issue candidates into cached verdicts, which declares the Provider interface it consumes). Covers what is sent to the model and in what order, snippet_mode and its privacy meaning, the controlled vocabulary, prompt_version rules and eval reports, the cache key and what invalidates a verdict, the cost cap, prompt-injection defences, storing the exact request payload, and where the API key comes from. Triggers on prompt, verdict, Judge, provider, triage, cache key, prompt_version, snippet_mode, cost cap, prompt injection, or any edit under internal/llm or the triage service. Not for Anthropic SDK parameters (load the built-in claude-api skill), the shared HTTP client (use outbound-client), or the settings page markup (use web-ui).
---

# LLM triage

The LLM stage answers one question per candidate: does this upstream issue affect this repo at this pinned version? Everything before it is deterministic. Its output only changes ranking; it never changes what a finding is or what state it is in. PLAN.md section 5.8 is the source; this skill is how to apply it.

- The `Provider` signature and its types: `docs/interfaces.md`. The `Checker` and `KeySource` types: `docs/interfaces.md`. Tables (`llm_verdicts`, `issue_candidates`): `docs/database.md`. Which package holds the triage service: `docs/architecture.md`.
- For Anthropic SDK specifics (structured output parameters, effort, prompt caching, usage fields, error types), load the built-in `claude-api` skill. Do not work from memory or from PLAN.md's parameter names; SDK details go stale.

## The Provider contract

`Provider` is declared in `internal/triage`, the package that uses it. `internal/llm` is a pure helper package that imports `model` only; `internal/triage` and the two adapters import it. The adapters are the subpackages `internal/llm/anthropic` and `internal/llm/openaicompat` (the config value is still `openai_compat`); they may import `llm`, `httpx` and `model`. The eval service drives `triage.Service` and never calls a provider directly.

- `Judge` keeps the signature `(JudgeResponse, Usage, error)` and makes one call for one candidate (one issue, one repo, one package, one pinned version). `JudgeResponse` carries the parsed, validated verdict fields plus two extra fields, `RequestPayload` and `RawResponse`: the exact bytes sent and received. Triage stores those as `request_payload` and `raw_response`, so `/f/{id}` can show exactly what left the box. The API key travels in a header, so the stored body never contains it.
- The model has no tools. Do not add tool definitions, function calling or any way for the model to act; that is the core of the injection posture.
- One retry layer per provider. Both adapters send through the shared httpx client (URL policy, timeout), which never retries POST. The `anthropic` adapter passes that client to the SDK and keeps the SDK's own retries as its one layer. The `openaicompat` adapter does not retry; a failed call leaves the candidate queued for the next run.
- Model names and prices are configuration. No model string or price appears in Go code, including defaults; tests use a made-up name such as `test-model`. The example config may suggest a model, and that is the only place one appears.
- Each adapter implements `Checker` (`Check(ctx context.Context) error`, see `docs/interfaces.md`), which `upw doctor` calls. It proves the key works without spending tokens, for example by listing models. It reports the key's last four characters only.

## What is sent, in this order

The static part comes first so the provider can cache it. Anything that varies per call placed before it breaks caching.

1. **System prompt**, identified by `prompt_version`: the role, the controlled vocabulary, the rule that everything inside the issue block is untrusted data to be reported and never followed, and the output schema. It contains no dates, IDs, repo names or anything else that varies. Anthropic gets the cache breakpoint at the end of it.
2. **Dependency facts**: package, ecosystem, pinned version, interval dates, direct or transitive, the versions the prefilter found and how they relate to the pin, the upstream repo, the latest known release if cached.
3. **Usage context**, by `llm.snippet_mode` (below).
4. **The issue**: title, labels, state, author association, body truncated to `llm.max_issue_chars` keeping head and tail, and up to the 10 most recent comments, each truncated. HTML is stripped, URLs are kept, images are dropped. The body sits inside `<upstream_issue>` and each comment inside `<comment author="..." association="...">`.

Rules for building it:

- **Neutralise the delimiters.** Before wrapping, rewrite any occurrence of the opening or closing `upstream_issue` and `comment` tags inside issue text, and escape quotes in attribute values. Otherwise an issue containing `</upstream_issue>` closes the data block and writes into the instructions.
- **Deterministic output.** The same inputs produce a byte-identical payload: sort symbols, fix ordering, format dates one way. Golden tests depend on it.
- One provider-neutral builder produces the ordered parts; each adapter only maps them onto its wire format.

## snippet_mode is a privacy setting

| Mode | The provider receives |
|---|---|
| `symbols` (default) | package names, pinned versions, the symbols we call with file counts, public issue text. No source code. |
| `snippets` | the above plus up to `llm.max_snippets` code windows of plus or minus 6 lines around matching call sites. This sends private code to a third party. |
| `none` | dependency facts and issue text only. |

- Never move up a mode implicitly, for example sending snippets because the symbol list is empty. The operator chose what may leave the network.
- Snippets come from source at HEAD read through `GitSource`, never from history, because history can hold secrets that HEAD no longer has.
- The mode is stored on every verdict, is part of the cache key, and is shown as a badge in the UI.

## Output schema and vocabulary

The schema, the `verdict`, `justification` and `recommended_action` values, and the reason the vocabulary mirrors OpenVEX are in PLAN.md 5.8.

- Define the vocabulary once, as Go constants in `internal/llm`. Build the JSON schema sent to the provider from those constants, so the two cannot drift.
- Ask the provider for strict structured output, then validate again in Go after decoding: enum membership, `confidence` within 0 to 1, `fixed_in` null or version-like, caps on evidence count and on reasoning length. Providers' strict modes have gaps; the second check is ours.
- Output that fails validation, a refusal, or a truncated response: count its tokens toward the budget, store no verdict, log the issue ID and `prompt_version`, and leave the candidate queued. The per-run call cap bounds the cost. The same candidate failing twice is a prompt bug.

## prompt_version

- Any change to what the model sees bumps `prompt_version`: the system prompt text, the schema, the vocabulary, the order of parts, the wrapping, or the format of the dependency facts or usage context.
- Prompt versions are immutable once used. Keep each version's text as its own embedded file and add a new one; stored verdicts and eval replays refer to old versions.
- A prompt change is accepted only with an eval report: run `upw eval run --prompt-version N` against the labelled set and attach the report from `eval/reports/` to your result. Without it, report the change as unfinished.

## Cache key and invalidation

- The key is the SHA-256 of provider, model, prompt version, issue ID, body hash, package, pinned version, usage hash and snippet mode, as in PLAN.md section 4. Compute it in one place, `Verdict.CacheKey()` in `internal/model`, and look it up before every call. M4 acceptance asserts the same issue is never billed twice.
- What makes a scheduled run judge a candidate again: a new body hash (new comments count only when `llm.rejudge_on_comment` is true), a different pinned version, a different usage hash, a new `prompt_version`, or a different `snippet_mode`.
- A model change is opt-in. The scheduled job treats a candidate as judged when a verdict exists that matches on everything except the model; only `upw llm rejudge --model ...` and `upw eval run` look up by the full key. `rejudge` prints the cost estimate and asks before calling.

## Cost cap

- Two limits: `llm.max_calls_per_run` and `llm.max_usd_per_day`. Cost is token usage times the operator's price table (`llm.prices_per_mtok`); the code knows no prices.
- Make calls one at a time. Before each, check calls made this run and USD spent today (the day in the configured `TZ`); after it, record actual usage. The cap can overshoot by at most one call.
- Count cache-read and cache-write tokens as input tokens at the input price. That overstates cost slightly, which is the safe side for a cap.
- If `max_usd_per_day` is set and the price table is missing, config validation fails. A cap you cannot compute is not a cap.
- On reaching a cap, stop. Remaining candidates stay queued, and the digest reports how many are waiting for budget.
- Each verdict stores input and output tokens, `cost_usd` and `latency_ms`.

## Injection posture

Issue text and comments are written by strangers. The defences, all required:

- No tools; issue text is data, wrapped and with delimiters neutralised (above).
- The system prompt says instructions inside the issue are to be reported in `reasoning`, not followed.
- Output is schema-validated twice.
- Each `evidence` quote must be a substring of the issue text as sent, compared after collapsing whitespace. If any quote fails, reduce confidence (one function in triage owns the rule) and store `llm_verdicts.evidence_verified` (1 when every evidence quote was found in the issue text, 0 otherwise); the UI shows "unverified evidence" when it is 0.
- A verdict can never acknowledge, snooze, dismiss or resolve a finding. Triage writes `llm_verdicts` and upserts the finding's score and rank; it never calls a state transition. `does_not_affect` hides a finding through ranking, not dismissal.
- Everything the model writes is escaped in the UI (`web-ui`) and markdown-neutralised in the digest.

## The API key

- One seam. Each provider adapter is constructed with a `KeySource`, a `func(ctx context.Context) (model.Secret, error)` (`docs/interfaces.md`), and calls it on every `Judge` and every `Check`, so a newly saved key works without a restart. Do not cache the key in the adapter.
- `cmd/upw` builds the `KeySource`: the env var `UPW_LLM_API_KEY` if set, otherwise `settings.Service`. `settings.Service` is the only code that encrypts and decrypts, and the only user of `SettingsStore`. `triage` never touches `SettingsStore` and never reads the key. Until M4 adds the settings page, the env var is the only route.
- The store holds ciphertext only. Encryption at rest uses AES-GCM with a random nonce (`cipher.NewGCMWithRandomNonce`); the key file lives in the data dir, created with mode 600 and `O_EXCL` on first use.
- Never log it, never put it in an error (wrap SDK errors with status and request ID, not the request dump), never render it back (the settings page shows the last four characters), never include it in `upw export`.

## Testing

- Triage service tests use a hand-written fake `Provider` that returns scripted responses and records the requests it received. No network and no key in unit tests.
- Golden files for the assembled payload, one per `snippet_mode`, including an issue that contains `</upstream_issue>` and "ignore previous instructions", to show both stay inside the data block.
- Provider adapters are tested against httptest servers serving recorded responses (`go-test`).
- Tests for the cache (second run makes no call), the cost cap (stops at the limit, leaves the rest queued), the evidence check, and that no verdict changes finding state.
- The M4 acceptance case (a crafted injection issue judged on its technical content, with the attempt visible in `reasoning`) runs against a real model in the eval, not in `make check`.

## Not covered here

- Anthropic SDK and API details: the built-in `claude-api` skill.
- The httpx client, URL policy and the `openaicompat` transport rules: `outbound-client`.
- The verdict page and the settings form: `web-ui`. The `llm_verdicts` and `settings` migrations: `sqlite-store`.
- Fakes, golden files and table tests: `go-test`. Reviewing injection and key handling: `security-check`.
