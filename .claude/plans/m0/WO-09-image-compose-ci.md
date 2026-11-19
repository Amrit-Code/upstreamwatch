# WO-09: Dockerfile, compose file and GitHub Actions CI

Milestone: M0
Agent: go-mech   Model: opus
Depends on: WO-01 (merged)
Report file: `.claude/plans/m0/WO-09-image-compose-ci.report.md`
Tickets: UPW-20, UPW-21

## Objective

The repository can build its container image, has the compose file the VM will run, and has a CI workflow that runs `make check` on every push and pull request and builds the image, pushing it to the registry on a version tag. Serves M0 checklist items 14, 19 and 20.

## Owned files

- `deploy/Dockerfile`
- `deploy/compose.yml`
- `deploy/.env.example`
- `.dockerignore`
- `.github/workflows/ci.yml`

Lead-owned, read-only for you: everything else, including `Makefile`, `go.mod`, `deploy/upw.example.yaml` (another work order writes it) and `.gitignore`.

## Interfaces

### Consumed

- `make check` (the one acceptance command; see `Makefile`).
- The binary: `go build ./cmd/upw`. Its `serve` subcommand, the `/health` route and the config path `/etc/upstreamwatch/upw.yaml` arrive in a later work order; write the image and compose file for them now.

### Produced

- Image `ghcr.io/amrit-code/upstreamwatch:<tag>`; container user UID 10001, GID 10001; config at `/etc/upstreamwatch/upw.yaml`; data volume at `/var/lib/upstreamwatch`; port 8080.

## Requirements

1. `deploy/Dockerfile`, built with the repository root as context (`docker build -f deploy/Dockerfile .`), multi-stage:
   - builder `golang:1.27-alpine`: copy `go.mod` and `go.sum`, `go mod download`, copy the source, `CGO_ENABLED=0 go build -trimpath -o /out/upw ./cmd/upw`;
   - runtime `alpine:3.22`: `apk add --no-cache git ca-certificates tzdata`, a group and user `upw` with GID and UID 10001, directories `/var/lib/upstreamwatch` (owned by `upw`, mode 0700) and `/etc/upstreamwatch`, the binary at `/usr/local/bin/upw`, `USER 10001:10001`, `WORKDIR /var/lib/upstreamwatch`, `EXPOSE 8080`, `ENTRYPOINT ["upw"]`, `CMD ["serve", "-config", "/etc/upstreamwatch/upw.yaml"]`.
   - both base images pinned by digest as `image:tag@sha256:...`, with the tag kept for readability. Resolve the digests yourself (`docker buildx imagetools inspect <image>`, or the registry HTTP API with `curl`). Use the multi-platform index digest. If neither route works, pin by tag only and report DONE_WITH_CONCERNS saying so.
2. `.dockerignore` keeps out `.git`, `.claude`, `docs`, `mvp-plan`, `data`, `data-dev`, `bin`, `upw.yaml`, `.env` and `*.local.md`, so no private file can enter the build context.
3. `deploy/compose.yml` is the service from `mvp-plan/PLAN.md` section 8 with these fixed points:
   - `image: ghcr.io/amrit-code/upstreamwatch:${UPW_VERSION:-latest}`, with a commented `build:` alternative (`context: ..`, `dockerfile: deploy/Dockerfile`) for use before the first tagged release;
   - `restart: unless-stopped`, `env_file: .env`, `environment: TZ: ${TZ:-UTC}`;
   - volumes `./upw.yaml:/etc/upstreamwatch/upw.yaml:ro` and `./data:/var/lib/upstreamwatch`;
   - `ports: ["127.0.0.1:8080:8080"]` exactly, with a comment that it must never become a bare `8080:8080` because `tailscale serve` is the only way in;
   - the healthcheck from the plan (`wget -qO- http://127.0.0.1:8080/health`, 60 s interval, 5 s timeout, 3 retries);
   - a comment that the mounted `upw.yaml` must set `http.listen: 0.0.0.0:8080` and `data_dir: /var/lib/upstreamwatch` inside the container, and that `./data` must be owned by 10001:10001;
   - no ntfy service.
4. `deploy/.env.example` lists `UPW_GITHUB_TOKEN_TARGETS`, `UPW_GITHUB_TOKEN_UPSTREAM`, `UPW_LLM_API_KEY`, `UPW_WEBHOOK_URL`, `UPW_SMTP_PASSWORD`, `UPW_VERSION` and `TZ=Europe/London`, each with an empty value (except `TZ`) and a one-line comment saying what it is and the minimum scope. No real or realistic-looking secret values.
5. `.github/workflows/ci.yml`:
   - triggers: `push` to `main`, `pull_request`, and tags matching `v*`;
   - top-level `permissions: contents: read`;
   - job `check` on `ubuntu-latest`: checkout, `actions/setup-go` with `go-version-file: go.mod`, then `make check`;
   - job `image` on `ubuntu-latest`, needing `check`: builds `deploy/Dockerfile` for `linux/amd64` on every run; on a `v*` tag it also logs in to `ghcr.io` with `GITHUB_TOKEN` and pushes `ghcr.io/amrit-code/upstreamwatch:<tag>` and `:latest`. Only this job has `packages: write`, and only the tag run pushes;
   - every third-party action is pinned to a full commit SHA with the version in a trailing comment (`uses: actions/checkout@<40-hex> # v6.x.y`). Look the SHAs up with `gh api repos/<owner>/<repo>/git/ref/tags/<tag>` (dereference annotated tags) and use the latest stable release of each action;
   - no secrets other than `GITHUB_TOKEN`; no `pull_request_target`.
6. Nothing in these files names a private repository, a hostname, a tailnet name or a token.

## Tests to include

None in Go. Verification is by the commands below.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 (unchanged by this work order) |
| `docker build -f deploy/Dockerfile -t upw:wo09 .` (only if a Docker daemon is already running; do not install or start one) | builds; `docker run --rm upw:wo09 -h` prints the usage and exits 0; `docker run --rm --entrypoint id upw:wo09` shows uid 10001; `docker run --rm --entrypoint git upw:wo09 --version` works |
| `docker compose -f deploy/compose.yml config` (same condition; create throwaway empty `deploy/.env` and `deploy/upw.yaml` for the command and delete them after) | valid; the published port is `127.0.0.1:8080` |
| `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/ci.yml'))"` or an equivalent YAML parse | parses |
| `grep -nE 'uses: [^@]+@' .github/workflows/ci.yml \| grep -vE '@[0-9a-f]{40} #'` | no output |
| `git status --porcelain` | only owned files and the report file |

If no Docker daemon is running, say so in the report and mark the Docker rows as not run; that is DONE_WITH_CONCERNS, not BLOCKED.

## Out of scope

- Running CI: the GitHub repository does not exist yet; the owner creates it.
- `staticcheck`, `govulncheck`, e2e fixtures, the weekly dogfood run (later milestones).
- Deploying to the VM, `tailscale serve` (UPW-24, owner approval needed).
- `deploy/upw.example.yaml`, `README.md`.

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `upw-deploy`.
Read: `mvp-plan/PLAN.md` section 8 and section 11 (CI and security paragraphs).

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

- 2026-11-19: `UPW_SMTP_PASSWORD` is the variable name for the email notifier's password.
