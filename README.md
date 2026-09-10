# InfiniteChance

[中文](README.zh-CN.md)

A personal-use token gateway + infinite canvas. Domain glossary: [CONTEXT.md](CONTEXT.md).

## Features

**Gateway (gateway/server)** — an OpenAI-compatible relay. Point your SDK's `base_url` at `http://localhost:8080/v1` and go:

- Chat `POST /v1/chat/completions`: synchronous forwarding; with `stream:true`, SSE frames pass through untouched. Billed as token price × ratio, with pre-deduction → settle-the-difference on completion → full refund on failure.
- Images `POST /v1/images/generations` (JSON) / `POST /v1/images/edits` (multipart): synchronous forwarding, billed per call (unit price × size factor × image count), routed only to channels with the `images` capability.
- Video `POST /v1/videos/generations` + `GET/POST /v1/videos/tasks/{id}` (poll/cancel): async task contract with a five-state machine (queued/running/succeeded/failed/canceled), billed only on success; tasks belong to the issuing key.
- Model catalog `GET /v1/models`: merges public models across all enabled channels; unaffected by circuit breaking.
- Multi-channel scheduling & circuit breaking: when one public model maps to multiple channels, candidates fail over by priority tiers with weighted-random split within a tier; each channel has its own breaker (consecutive transient failures trip it open → cooldown → half-open single-flight probe); when every candidate is open, requests are rejected with 503 `model_unavailable`.
- Pricing & quota: dual-track pricing (token track / per-call·per-second track; unpriced models are always rejected with `model_not_priced`); quota is tracked in micro-USD with a ledger entry for every change; API keys are `sk-` + 40 random characters, stored hashed only, with expiry and revocation.
- Usage audit: per-request logs (channel/model snapshots, price snapshot, upstream error summary, `X-InfiniteChance-Source` source tag — canvas writes look like `canvas=<id> task=<…>`) plus `GET /admin/usage/summary` rollups by day/model/channel; the admin console's Usage Audit page shows details and all three rollup buckets.

**Creation canvas (canvas/server + canvas/web)**:

- vue-flow editor: prompt/image/video nodes plus edges, whole-graph JSON with optimistic-lock autosave; canvas list and CRUD.
- Text-to-image and image-to-video: orchestrated by canvas/server workers (FIFO claiming, concurrency cap, retry on failure, cancel, orphan re-queue on restart) — tasks keep running with the browser closed; artifacts land in the asset library and are written back to nodes; video supports a reference image and duration.
- Prompt generation and video reverse-prompt: synchronous chat calls billed by token; admin-maintained prompt templates (with a `{topic}` placeholder) take effect immediately on create/update/disable — no cache, no sync step.
- Asset library: on task success, artifacts are copied into self-hosted storage (S3-compatible `objectstore` interface; the MVP ships a local volume, with no reliance on vendor temporary URLs); cross-canvas reuse goes through content addressing at `/assets/{id}/content`; the canvas asset panel and the admin assets page share one list API.

**Admin console (admin-web)**: dashboard (health of both services), channels (with one-click connectivity test), API keys (create/revoke/quota top-up), usage audit, prompt templates, assets. Model pricing currently goes through the admin API (`/admin/pricing`) only — no page yet.

## API surface

| Mount | Auth | Contents |
| --- | --- | --- |
| gateway `/v1/*` | API key (Bearer `sk-…`) | OpenAI-compatible relay, see above; errors are OpenAI error objects with codes like `invalid_api_key` / `insufficient_quota` / `model_not_priced` |
| gateway `/admin/*` | JWT session | CRUD for channels, API keys, model prices, and prompt templates; usage log list & rollups |
| gateway `/auth/*` | `status`/`init`/`login` public; `me` needs JWT | single-admin session |
| canvas `/canvases` | JWT session | canvas CRUD + `PUT /:id/graph` whole-graph save; `:id/tasks` create/list/retry/cancel; `:id/generate-prompt`, `:id/reverse-prompt` synchronous actions |
| canvas catalogs | JWT session | `/image-models`, `/video-models`, `/prompt-templates` (enabled only), `/prompt-models` (token track) |
| canvas `/assets` | list/delete need JWT; `/:id/content` public | asset library; content is deliberately unauthenticated — `<img>/<video>` elements cannot attach an Authorization header, and the artifacts are the same public media the vendor delivered |

Deployed reverse proxy: admin-web's `/api` → gateway and `/canvas-api` → canvas; canvas-web's `/api` → canvas; dev proxies mirror this.

## Layout

```
gateway/server   Go+Gin gateway entrypoint (OpenAI-compatible API)
canvas/server    Go+Gin canvas persistence & task orchestration entrypoint
canvas/web       Vue3 creation-canvas frontend (:5174 dev / :8091 deployed)
admin-web        Vue3 unified admin console (:5173 dev / :8090 deployed)
packages/api     shared frontend request layer (@infinitechance/api)
packages/ui      shared frontend components (HealthCard)
deploy/          deployment artifacts: nginx configs + backup/restore scripts
desktop/         Wails v3 desktop shell (SQLite; canvas main window + admin config window)
```

The Go side is a single module (`github.com/gachal/InfiniteChance`) with two binaries; `internal/` holds code shared by both services. The frontend is a pnpm workspace.

## One-command deploy

Any fresh machine only needs Docker:

```bash
cp .env.example .env   # optional; every setting has a built-in default
docker compose up -d --build
```

One command brings up the six-piece stack: MySQL, Redis, gateway, canvas, plus the two frontends — the multi-stage Dockerfile first runs `pnpm build` inside the container to produce the two SPA bundles, then hands them to two nginx runtime containers that serve the static files and reverse-proxy their backends; **the runtime containers contain only images and build artifacts — no host source code, Node, or pnpm required**.

Then open the admin console at `http://localhost:8090` and finish the init wizard (two steps):

1. Create the single admin account (the password is stored only as a bcrypt hash);
2. Enter the first vendor channel (OpenAI-compatible BaseURL + key + optional model mapping; skippable).

Afterwards, create a service-level key under "API Keys", put it into `CANVAS_SERVICE_KEY` in `.env`, and run `docker compose up -d` once more — canvas AI actions become available.

For public/long-lived deployments: generate `JWT_SECRET` with `openssl rand -hex 32`, put it in `.env`, and set `JWT_SECRET_REQUIRED=true` (services refuse to start when the secret is missing). All settings and comments: [.env.example](.env.example).

Frontend development can still use dev servers (hot reload): `pnpm install && make dev-admin` (:5173) / `make dev-canvas` (:5174), reaching 8080/8081 through the vite proxies.

## Desktop app

`desktop/` packages the same two services into one native app (Wails v3 beta): **one process, one SQLite file — no Docker, no MySQL, no Redis** (Redis was only ever a health-check ping). The canvas opens as the main window; the admin console is an on-demand configuration window (app menu, and auto-opened on first run). The gateway keeps serving the OpenAI-compatible `/v1` on `127.0.0.1:8080` for external SDKs; ports are configurable via `gateway_port`/`canvas_port` in `config.json`, and an occupied port fails the boot loudly instead of drifting.

```bash
make desktop                                # builds both SPAs, embeds them, -> desktop/build/bin/InfiniteChance
open desktop/build/bin/InfiniteChance
```

Data lives in the OS application-data directory — `~/Library/Application Support/InfiniteChance` on macOS, `%APPDATA%\InfiniteChance` on Windows, `~/.config/InfiniteChance` on Linux — holding `app.db`, `assets/` and `config.json`; copying that directory is a full backup. First boot auto-provisions the canvas service key and a random `JWT_SECRET`; a revoked service key re-provisions on the next start. Desktop data is independent of the Docker stack (fresh init; see [ADR 0001](docs/adr/0001-sqlite-dual-dialect-stores.md) for the SQLite dual-dialect decision). Dev mode: `make dev-desktop`, with `INFINITECHANCE_DEV=1` plus `INFINITECHANCE_DEV_CANVAS`/`INFINITECHANCE_DEV_ADMIN` to point the windows at the vite dev servers, and `INFINITECHANCE_DATA_DIR` to relocate the data directory.

## Ports

| Service | Port | Notes |
| --- | --- | --- |
| admin-web (deployed) | 8090 | admin console static hosting; init wizard entry |
| canvas-web (deployed) | 8091 | infinite canvas static hosting |
| gateway/server | 8080 | gateway API (OpenAI-compatible `/v1`) |
| canvas/server | 8081 | canvas API |
| admin-web (dev) | 5173 | admin console dev server |
| canvas/web (dev) | 5174 | canvas frontend dev server |
| MySQL | host 3307 → container 3306 | dodges the common local 3306 occupancy |
| Redis | host 6380 → container 6379 | dodges the common local 6379 occupancy |

All ports can be overridden via `.env` (`ADMIN_WEB_PORT`, `CANVAS_WEB_PORT`, `GATEWAY_PORT`, `CANVAS_PORT`, `MYSQL_PORT`, `REDIS_PORT`).

When running the Go services directly on the host (`go run ./gateway/server`), they default to `localhost:3306/6379`; if the infrastructure runs on compose-mapped ports, set:

```bash
export MYSQL_DSN='root:infinitechance@tcp(localhost:3307)/infinitechance?parseTime=true'
export REDIS_ADDR=localhost:6380
```

## Backup & restore

`deploy/backup.sh` packs three state stores into one manifest-carrying directory (consistent MySQL logical dump, Redis RDB, a full tar of the assets volume, with a timestamp/git-commit/SHA-256 manifest), and `deploy/restore.sh` pours a directory back into the running stack (overwrite-style; it confirms before acting):

```bash
make backup                          # → backups/YYYYmmdd-HHMMSS/
make restore DIR=backups/YYYYmmdd-HHMMSS   # append Y=1 to skip the interactive confirmation
```

Both require the compose stack to be running (they use in-container mysqldump/redis-cli/tar — no database tools on the host). While the assets volume is being packed, canvas pauses for a few seconds (to prevent a torn tar; in-flight canvas tasks re-run via the restart-recovery mechanism); there is no global consistency point across the three stores, so run during off-peak hours. Periodic backup example (daily at 3 AM):

```
0 3 * * * cd /path/to/InfiniteChance && deploy/backup.sh >> backups/backup.log 2>&1
```

Restore drill: backup → `docker compose down -v` to wipe all data volumes → `docker compose up -d` → `deploy/restore.sh <dir> -y` → verify that login, channels, Redis keys, and assets are all back.

## Health-check contract

`GET /healthz` (identical on both services): 200 when every dependency is reachable, 503 otherwise:

```json
{
  "service": "gateway",
  "status": "ok",
  "checks": {
    "mysql": { "status": "up" },
    "redis": { "status": "up" }
  }
}
```

When a dependency is unreachable, its `checks` entry reads `"status": "down"` with an `error` summary. compose attaches `/healthz` health checks to gateway/canvas, and the two frontend containers start only after their backends turn healthy — if the page loads, the service is up.

## Admin auth

A single admin account + JWT sessions, shared by gateway and canvas:

- **First use**: visiting the admin console on a fresh database shows the init wizard — create the single admin account and, on the way, enter the first vendor channel; the wizard never appears again after initialization.
- **Login**: the gateway verifies credentials and issues an HS256 JWT (valid for 7 days); requests without a token, with a bad signature, or expired always get a standard 401 (`{"error":{"code","message"}}` + `WWW-Authenticate`).
- **Cross-service verification**: canvas/server verifies gateway-issued JWTs with the same secret — the `JWT_SECRET` environment variable must be identical in both services. compose passes the host's `JWT_SECRET` through; when unset, both services fall back to a built-in dev secret (local development only). For production, set `JWT_SECRET` and enable `JWT_SECRET_REQUIRED=true` — the latter refuses to start when the secret is missing, so you never go live with a public key.

`GET /auth/status`, `POST /auth/init`, and `POST /auth/login` are public endpoints; `GET /auth/me` requires a Bearer token (offered by both gateway and canvas).

## Common commands

```bash
make up          # compose brings up the full stack (six services)
make down        # stop (data volumes kept)
make backup      # back up MySQL/Redis/assets volume → backups/<timestamp>/
make restore     # restore from a backup (needs DIR=, see above)
make test        # go vet + go test + pnpm -r test
make lint        # go vet + pnpm -r lint
```

Configuration is injected via environment variables: `PORT`, `MYSQL_DSN`, `REDIS_ADDR`, `JWT_SECRET` (services); the compose stack's ports/passwords/keys come from `.env` (see `.env.example`).
