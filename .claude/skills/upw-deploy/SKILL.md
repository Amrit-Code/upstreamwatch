---
name: upw-deploy
description: Runbook for building the upstreamwatch container image and running the deployed instance on the homelab VM - prerequisites, first boot, upgrade, rollback, backup and export, and promotion to the lab tier, plus the list of actions that need the owner's yes first. Triggers on "deploy", "first boot", "upgrade the VM", "new release", "docker compose", "Dockerfile", "image", "rollback", "restore a backup", "export verdicts", "tailscale serve", "health check", "promote to lab", or M0 and M5 deployment acceptance steps. Not for creating, destroying or reaching VMs in general (load the homelab skill, which is the authority), or building and testing code, which happens on the Mac (use go-test).
---

# Deploying upstreamwatch

Development and tests run on the Mac. The homelab VM runs the deployed instance only: no `go test` there, no source edits there. PLAN.md section 8 is the source; where it describes a dev loop on the VM, that is superseded.

Load the `homelab` skill before touching a VM. It is the authority for creating VMs, reaching them over Tailscale SSH, tiers, snapshots and fleet docs. This runbook does not repeat it and names no hosts; `<vm>` below is the VM's name as the homelab skill reports it.

## Ask the owner first

Stop and ask before any of these, even when a work order seems to imply them:

- Creating or destroying a VM.
- Anything touching the tailnet: tags, retagging, ACL policy, `tailscale serve` on a different host or port, any public exposure. The instance stays tailnet-only; no Cloudflare tunnel.
- Promotion to the `lab` tier, and editing the owner's infra repo or fleet docs.
- Putting real tokens or keys on the box. The owner supplies `.env` and `upw.yaml`; you copy files you are given and never print their contents.
- Restoring a backup over live data, or any rollback that discards data written since the backup.
- Running commands that spend LLM money outside the schedule: `upw llm rejudge`, `upw eval run`.
- Pointing the instance at a local model server. The homelab's own model host is not to be used unless the owner says so.

## The image

- `deploy/Dockerfile`, multi-stage: `golang:1.27-alpine` builds with `CGO_ENABLED=0 go build -trimpath ./cmd/upw`; the runtime stage is `alpine:3.22` with `git` and `ca-certificates`, a fixed non-root UID and GID, `ENTRYPOINT ["upw"]`, `CMD ["serve"]`.
- Alpine rather than distroless, because `internal/gitsrc` shells out to `git` at runtime, and the compose health check uses busybox `wget`.
- Pin both base images by digest, so a rebuild cannot silently pull a different image.
- The deployed instance runs the tagged image that CI (GitHub Actions) builds on a tag and pushes to the registry, `ghcr.io/amrit-code/upstreamwatch:<tag>` (amd64). Before the first tagged release exists, building the image on the VM with `docker compose build`, from a clone of the public repo at a named commit and compose's `build:` in place of `image:`, is acceptable. That is an image build, not the development loop, which stays on the Mac. Do not install Docker on the Mac.

## Prerequisites

1. The VM exists, created per the homelab skill in the `dev` tier, and `ssh root@<vm>` works.
2. On the Mac, ready to copy: `compose.yml` from `deploy/`, and the owner's `upw.yaml` and `.env`. The last two name private targets and hold secrets; they never enter the repo.
3. `make check` passes on the Mac for the commit being deployed.

## First boot

On the VM (commands from PLAN.md section 8):

```bash
apt-get update && apt-get install -y docker.io docker-compose-v2 ufw git
ufw default deny incoming && ufw default allow outgoing && ufw allow in on tailscale0 && ufw --force enable
mkdir -p /srv/upstreamwatch/data && cd /srv/upstreamwatch
```

From the Mac, `scp` `compose.yml`, `upw.yaml` and `.env` into `/srv/upstreamwatch/`. Then on the VM:

```bash
chmod 600 .env upw.yaml
chown -R <image UID>:<image GID> data && chmod 700 data   # the container user must own its data dir
docker compose up -d
docker compose exec upw upw doctor          # every check passes; tokens shown as last four only
docker compose exec upw upw run all --once
tailscale serve --bg --https=443 http://127.0.0.1:8080
```

Inside the container the config sets the listen address to `0.0.0.0:8080` (the config default, `127.0.0.1:8080`, applies only when the binary runs directly), and compose publishes it as `127.0.0.1:8080:8080`, on the host's loopback only. The check is that the service is not reachable from outside the host except through `tailscale serve`: from the Mac, a `curl` to the VM's address on port 8080 must be refused. Do not change the published port to a bare `8080:8080`.

Done when the tailnet URL's `/health` returns `{"ok":true,...}` (M0 acceptance) and `upw doctor` passes with the real tokens.

If git reports "dubious ownership" inside the container, the data dir's owner does not match the image's UID. Fix the ownership; do not add `safe.directory=*`.

## Upgrade

1. Back up first: `docker compose exec upw upw run backup`, then the export below. The backup is your rollback if a migration runs.
2. Note the current image tag and digest.
3. Set the new tag and digest (the deployed compose pins the image by digest, PLAN.md section 11), then `docker compose pull && docker compose up -d`.
4. Watch `docker compose logs upw --since 10m`, run `upw doctor`, check `/health`.

Migrations run at startup and are additive until 1.0. The binary refuses to start on a database newer than itself.

## Rollback

- **No migration in the new release:** set the previous tag and digest, `docker compose up -d`.
- **A migration ran:** the old binary will refuse the newer database, so rollback means restoring the pre-upgrade backup. Ask the owner first, because verdicts, acks and labels written since the backup are lost. Then:

```bash
docker compose stop upw
mv data/<db file> data/<db file>.failed            # move its -wal and -shm files with it
cp data/backups/<pre-upgrade backup> data/<db file>
# set the previous tag and digest
docker compose up -d
```

## Backup and export

- The nightly `backup` job writes `VACUUM INTO data/backups/`, keeping 7. They sit on the same disk as the database, so a disk failure loses them; say so whenever backups come up. Fleet backups are the owner's, on request, per the homelab skill.
- Monthly and before every upgrade: run `upw export verdicts`, `upw export labels` and `upw export state`, one kind per invocation, each written to stdout: `docker compose exec upw upw export verdicts > verdicts-YYYYMM.json`, and likewise for `labels` and `state`. Then `scp` the three files to the Mac, outside the repo. Together they hold everything that cannot be rebuilt (verdicts, finding state, eval labels) and names private usage, so it never enters the public repo.
- Copy only the exports off the box, not `data/`. The data dir holds the bare clones of private repos (which may contain secrets in history) and the key file that decrypts the stored LLM key.

## Promotion to lab

Only on the owner's yes, after a few weeks of digests with no surprises (M5 acceptance). Follow the homelab skill's checklist for putting a service live; the values specific to upstreamwatch are:

- Health check for the patch playbook: `health_check_url: http://127.0.0.1:8080/health`, `health_check_status: 200`.
- A snapshot named `pre-lab` before promoting, stated plainly as not a backup.
- Tailscale tag stays `tag:dev`; nothing in the MVP needs to reach other tailnet nodes. A retag is a policy change for the owner.
- Add the instance to the fleet docs the homelab skill names.

## Not covered here

- Creating, reaching and destroying VMs, tiers, snapshots, Ansible: the `homelab` skill.
- What the tables hold and why the three non-rebuildable ones matter: `docs/database.md` and the `sqlite-store` skill.
- Milestone acceptance commands: `milestone-accept`. Reviewing the image and compose for supply-chain issues: `security-check`.
