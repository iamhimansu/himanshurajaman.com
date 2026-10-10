# himanshurajaman.com

Personal portfolio, resume, and blog with a minimal GitHub-backed CMS. A single
Go binary serves a JSON API and the embedded React frontend; SQLite is a
disposable read cache rebuilt from the content repo on boot.

- **Backend:** Go, chi, modernc.org/sqlite (pure Go); posts are Editor.js JSON
  documents rendered to sanitized HTML server-side (chroma highlighting,
  bluemonday).
- **Frontend:** React + Vite + TypeScript, React Router, TanStack Query, plain CSS.
- **Serving:** the built frontend (`web/dist`) is embedded with `go:embed`, so
  there is one deployable unit.

## Prerequisites

- Go (see `go.mod` for the version)
- Node.js 18+ and npm

## Build order (important)

The frontend must be built **before** the Go binary, because
`web/embed.go` embeds `web/dist` at compile time with `//go:embed all:dist`.
`web/dist` is a build artifact and is **not** committed (see `.gitignore`), so a
fresh checkout must build the frontend first.

```sh
make build      # cd web && npm ci && npm run build, then CGO_ENABLED=0 go build ./...
```

Or a single server binary at `bin/server`:

```sh
make server     # frontend build, then CGO_ENABLED=0 go build -o bin/server ./cmd/server
```

## Development

Two processes: the Go API on `:8080` and the Vite dev server on `:5173`. Vite
proxies `/api` and `/images` to the Go server (see `web/vite.config.ts`), so the
React app uses the same relative URLs in dev and prod.

```sh
make dev
```

Or run them in two terminals:

```sh
# terminal 1 — API against the bundled sample content
CONTENT_SOURCE=local CONTENT_LOCAL_DIR=./sample-content go run ./cmd/server

# terminal 2 — frontend dev server (open http://localhost:5173)
cd web && npm run dev
```

For a production-like run (binary serving the embedded build), build first, then:

```sh
make run        # CONTENT_SOURCE=local CONTENT_LOCAL_DIR=./sample-content go run ./cmd/server
```

Configuration is environment-only; see `.env.example`.

## Routes

Public SPA routes: `/` (home), `/resume`, `/blog`, `/blog/:slug`, `/search`.
`/admin/*` is the lazy-loaded CMS (Google sign-in + Editor.js post/resume
editing), code-split into its own chunk so visitors never download it.

The server serves hashed build assets from `/assets/*` with a long immutable
cache; every other non-API path falls back to the embedded `index.html` so
client-side deep links and refreshes work. `/api/*` and `/images/*` return
`503 {"status":"warming"}` until the first content ingest completes.

## Admin / CMS (optional)

The admin surface (Google sign-in + a GitHub write-through CMS) is optional and
off by default. The binary boots and serves the full public site with no admin
configuration; while disabled, every `/auth/*` and `/api/admin/*` request
returns `503 {"error":"admin disabled"}`.

Enable it by setting all four of `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`,
`ADMIN_EMAIL` and `SESSION_SECRET` (see `.env.example`). Setting some but not
all of them fails fast at boot, naming the missing variables. `SESSION_SECRET`
must be at least 32 bytes — generate one with `openssl rand -base64 48`. Set
`COOKIE_SECURE=true` in production (HTTPS) and `false` for `http://localhost`.

GitHub-backed writes additionally require `CONTENT_SOURCE=github`; with local
content the admin UI is read-only (`writesEnabled=false`) and the GitHub-write
endpoints return `503 {"error":"writes disabled: content source is not
github"}`. Resync and the resume/read views still work.

### Google OAuth client

Create a Web-application OAuth client in Google Cloud and register both redirect
URIs:

- `https://himanshurajaman.com/auth/callback`
- `http://localhost:8080/auth/callback`

Only the single `ADMIN_EMAIL` Google account (with a verified email) may sign
in; any other verified Google login is rejected.

### GitHub token

Once admin writes are enabled, `GITHUB_TOKEN` needs **Contents: Read AND Write**
on the content repo (read-only was enough for public ingest). Use a fine-grained
PAT scoped to that single repo. Fine-grained PATs allow a maximum expiry of 366
days — rotate the token before it expires and update `GITHUB_TOKEN`.

## Deployment (Render)

The app deploys to [Render](https://render.com) as a single **free Docker web
service** built from the repo's `Dockerfile` (frontend build → static Go binary
→ distroless non-root runtime). The repo ships a best-effort `render.yaml`
Blueprint; **verify it against Render's current
[Blueprint spec](https://render.com/docs/blueprint-spec)** before relying on it —
the dashboard is the fallback for anything the Blueprint gets wrong.

### 1. Prerequisite — the content repo

The private content repo (`content.himanshurajaman.com`) must exist with the
**current** layout (posts are Editor.js JSON now, not Markdown):

```
posts/<slug>.json      # Editor.js documents: {"meta": {...}, "content": {...}}
resume/resume.json
settings/site.json
images/
```

You can seed it from the local `sample-content/` directory (already `.json`).

### 2. Create the web service

New → **Web Service** (or **New Blueprint** to apply `render.yaml`), connect the
**public code repo**, choose **Docker**, branch `main`, plan **Free**, and set
`healthCheckPath` to `/healthz` (liveness; it never calls GitHub).

Leave **PORT** unset: Render injects it (default `10000`) and the server binds
`$PORT` on all interfaces. Do not hardcode a port anywhere.

### 3. Environment variables (set in the dashboard)

Secrets are `sync: false` in `render.yaml`, so Render prompts for them and no
values are committed. Grouped:

- **Content:** `CONTENT_SOURCE=github`, `CONTENT_REPO=<owner>/content.himanshurajaman.com`,
  `CONTENT_REF=main`, `GITHUB_TOKEN` (fine-grained PAT, **Contents: Read + Write**,
  that repo only).
- **Auth / site:** `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `ADMIN_EMAIL`
  (the only account allowed in), `SESSION_SECRET` (≥ 32 bytes —
  `openssl rand -base64 48`), `BASE_URL=https://himanshurajaman.com`,
  `COOKIE_SECURE=true`.
- **Webhook:** `GITHUB_WEBHOOK_SECRET` (enables `POST /webhook/github`).
- **Contact / SMTP:** `SMTP_HOST`, `SMTP_PORT` (587), `SMTP_USERNAME`,
  `SMTP_PASSWORD`, `CONTACT_TO`, `CONTACT_FROM` — but see the free-tier SMTP
  caveat in step 8.

### 4. Google OAuth redirect

Add the production redirect URI to the OAuth client (keep the localhost one for
dev):

```
https://himanshurajaman.com/auth/callback
```

### 5. GitHub webhook (push-to-resync)

On the **content** repo → Settings → Webhooks → Add webhook:

- Payload URL: `https://himanshurajaman.com/webhook/github`
- Content type: `application/json`
- Secret: the same value as `GITHUB_WEBHOOK_SECRET`
- Events: **Just the `push` event**

A push then triggers a re-ingest, so published content updates without a redeploy.

### 6. Custom domain + DNS

In the Render dashboard add both `himanshurajaman.com` and
`www.himanshurajaman.com`; Render auto-provisions TLS. Then at your DNS provider:

- **Apex** `himanshurajaman.com`: an **ALIAS/ANAME** record to
  `<name>.onrender.com` — a bare apex cannot be a `CNAME`. If your provider has
  no ALIAS/CNAME-flattening, use an **A record** to Render's load-balancer IP
  shown in the dashboard.
- **www**: a **CNAME** to `<name>.onrender.com`.

The exact `<name>.onrender.com` target (and any A-record IP) come **from the
Render dashboard** — don't guess them.

### 7. Keep-alive pinger (free tier sleeps)

Free web services spin down after ~15 minutes idle (~1 min cold start). Add an
external cron pinger (e.g. [cron-job.org](https://cron-job.org)) that hits
`https://himanshurajaman.com/healthz` every ~14 minutes.

**Caveat:** a 24/7 pinger consumes essentially all of the **750 free
instance-hours per workspace per month**, so don't run other free services in
the same Render workspace or they'll compete for those hours.

### 8. Contact form on free (SMTP is blocked)

Render **free** web services **block outbound SMTP** (ports 25/465/587), so the
built-in SMTP mailer will **not** deliver in production on free. The SMTP mailer
and the `mailto:` fallback are left as-is; you choose how to handle sending:

- **Option A — HTTP email API:** swap the `Mailer` (`internal/contact`) for a
  transactional email provider's HTTP API (free tier). The `Mailer` interface
  makes this a drop-in — no handler or `Service` changes.
- **Option B — Disabled:** leave the `SMTP_*` vars unset. `POST /api/contact`
  returns `503` and the home page shows the `mailto:` fallback instead of the
  form (the site stays fully usable).

Until Option A is in place, run with contact disabled (Option B) on free.

### What the Dockerfile does

1. **node:22-slim** — `npm ci` + `npm run build` → `web/dist`.
2. **golang:1.26-bookworm** — copies the module + the built `web/dist`, then
   `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build` (Render builds amd64). The
   `go:embed` step bakes `web/dist` into the binary.
3. **gcr.io/distroless/static-debian12:nonroot** — copies just the static
   binary. Distroless ships CA certificates (for HTTPS to GitHub/Google/email)
   and runs as a non-root user. The image honors `$PORT`.
