Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m0/WO-09-image-compose-ci.md
Files changed:
- deploy/Dockerfile (created)
- deploy/compose.yml (created)
- deploy/.env.example (created)
- .dockerignore (created)
- .github/workflows/ci.yml (created)

Commands run:
- `make check` -> exit 0
  ```
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/config	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/model	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/query	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/sched	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/store	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/web	[no test files]
  ==> arch-lint
  go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false
  module: github.com/Amrit-Code/upstreamwatch
  linters:
     On | Base: component imports # always on
     On | Advanced: vendor imports # switch 'allow.depOnAnyVendor = false' (or delete) to on
     On | Advanced: method calls and dependency injections # switch 'allow.deepScan = true' (or delete) to on

  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
  ```
  (An earlier uncached run gave the same output with real test timings.)
- `docker info --format '{{.ServerVersion}}'` -> failed
  ```
  failed to connect to the docker API at unix:///Users/amrit/.docker/run/docker.sock; check if the path is correct and if the daemon is running: dial unix /Users/amrit/.docker/run/docker.sock: connect: no such file or directory
  ```
  No Docker daemon is running, so per the work order the `docker build` / `docker run` acceptance row was NOT RUN (nothing installed or started).
- `docker compose -f deploy/compose.yml config` (with throwaway empty `deploy/.env` and `deploy/upw.yaml`, deleted afterwards; `docker compose config` needs no daemon, so it was run even though the daemon condition was not met) -> exit 0
  ```
  name: deploy
  services:
    upw:
      environment:
        TZ: UTC
      healthcheck:
        test:
          - CMD
          - wget
          - -qO-
          - http://127.0.0.1:8080/health
        timeout: 5s
        interval: 1m0s
        retries: 3
      image: ghcr.io/amrit-code/upstreamwatch:latest
      networks:
        default: null
      ports:
        - mode: ingress
          host_ip: 127.0.0.1
          target: 8080
          published: "8080"
          protocol: tcp
      restart: unless-stopped
      volumes:
        - type: bind
          source: <worktree>/deploy/upw.yaml
          target: /etc/upstreamwatch/upw.yaml
          read_only: true
          bind: {}
        - type: bind
          source: <worktree>/deploy/data
          target: /var/lib/upstreamwatch
          bind: {}
  networks:
    default:
      name: deploy_default
  exit 0
  ```
  Published port is host_ip 127.0.0.1, 8080. `ls -a deploy` afterwards: `.env.example compose.yml Dockerfile` (throwaway files removed, no `data/` created).
- `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/ci.yml'))"` -> exit 0 (printed `parsed`). Parsed `on`: `{'push': {'branches': ['main'], 'tags': ['v*']}, 'pull_request': None}`; top-level permissions `{'contents': 'read'}`; job permissions `{'check': None, 'image': {'contents': 'read', 'packages': 'write'}}`.
- `grep -nE 'uses: [^@]+@' .github/workflows/ci.yml | grep -vE '@[0-9a-f]{40} #'` -> no output (exit 1 from grep, meaning no unpinned line)
- `git status --porcelain --untracked-files=all` -> (before this report was written)
  ```
  ?? .dockerignore
  ?? .github/workflows/ci.yml
  ?? deploy/.env.example
  ?? deploy/Dockerfile
  ?? deploy/compose.yml
  ```
- Base image digests, via the Docker Hub registry API (HEAD on `/v2/library/<image>/manifests/<tag>` with the OCI index Accept header) -> both `content-type: application/vnd.oci.image.index.v1+json` (multi-platform index):
  - `golang:1.27-alpine` -> `sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280`
  - `alpine:3.22` -> `sha256:49340b346798875b5592e5d420241f2f9cfba5f47e62ebb1446a1b34734e898f`
- Action SHAs: `gh api repos/<owner>/<repo>/releases/latest` then `gh api repos/<owner>/<repo>/git/ref/tags/<tag>` (both are lightweight tags pointing at commits; confirmed with `gh api .../commits/<sha>`):
  - `actions/checkout` v7.0.1 -> `3d3c42e5aac5ba805825da76410c181273ba90b1` (committed 2026-07-17)
  - `actions/setup-go` v7.1.0 -> `d8cbb8045d7f43706b9177cceb36c1f91a6644d5` (committed 2026-11-17)

Decisions:
- Image build, login and push in CI use the `docker` CLI preinstalled on `ubuntu-latest` (`docker build --platform linux/amd64`, `docker login --password-stdin`, `docker tag`/`docker push`) instead of docker/login-action, setup-buildx-action and build-push-action, so the only third-party actions are checkout and setup-go.
- Login and push steps run only when `github.event_name == 'push' && startsWith(github.ref, 'refs/tags/v')`; the tag name reaches the shell through an `env:` variable, not inline `${{ }}` in `run:`, to avoid template injection.
- `actions/checkout` uses `persist-credentials: false` in both jobs; nothing later needs the token in git config.
- `.dockerignore` also excludes `**/upw.yaml`, `**/.env`, `**/*.local.md` and `deploy/data`: with compose's commented `build:` alternative, the operator's `upw.yaml`, `.env` and `data/` sit in `deploy/` inside the build context, and the root-only patterns would not catch them. `data` was not made `**/data` so a future source directory with that name is not dropped from the build.
- Runtime user created with `adduser -S -D -H -h /var/lib/upstreamwatch -s /sbin/nologin` (system account, no password, no home created by adduser; the home is the data dir created with `install -d -o upw -g upw -m 0700`), so git inside the container gets a HOME it can write.
- No `VOLUME` instruction in the Dockerfile: the requirement's list of runtime steps does not include one, and compose mounts the data dir.
- The commented ntfy block from PLAN.md section 8 was dropped entirely ("no ntfy service").
- `.env.example` comment for `UPW_VERSION` and `TZ` says "not a secret" in place of a minimum scope.

Outside scope:
- PLAN.md section 8 still says `CMD ["serve"]` and the `upw-deploy` skill's image section says `CMD ["serve"]` and lists only `git` and `ca-certificates`; the work order (followed) has `CMD ["serve", "-config", "/etc/upstreamwatch/upw.yaml"]` and adds `tzdata`. The skill and plan text could be brought in line by docs-sync.
- PLAN.md section 8 "Backups" shows `upw export verdicts labels state > export-YYYYMM.json` in one invocation, while the `upw-deploy` skill says one kind per invocation. Not touched by this work order.
- PLAN.md section 11 lists `deploy/homelab.md`; this work order does not create it.
- Docs rows to update after acceptance: `docs/development.md` (CI workflow and image build), and wherever `docs/architecture.md` describes deployment, if it does.

Concerns:
- No Docker daemon was running, so the image was not built and `docker run --rm upw:wo09 -h`, `--entrypoint id` and `--entrypoint git ... --version` were not run. The Dockerfile is unverified by an actual build. Until the `serve` subcommand lands in a later work order, the image's default CMD exits 2 with "unknown command" (expected by the work order).
- `actions/setup-go` v7.1.0, the latest stable release as asked, was published two days ago (2026-11-17). If the lead prefers a release with some soak time, the previous release's SHA would need to be looked up.
- The `image` job holds `packages: write` on every run (permissions cannot be conditional); only the tag run uses it. On `pull_request` from forks GitHub downgrades the token to read-only anyway.
