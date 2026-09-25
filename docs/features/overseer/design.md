# Overseer — design

How the shaped idea is built. Dump: `docs/drafts/overseer.md`. Weigh: `docs/drafts/overseer-weigh.md`.
Design settles the *how*; it does not re-open the *what*.

## Anchor & known invariants (inherited, not re-argued)

- **Anchor:** a god's-eye control room for one user that shows the whole app at once, outlives
  the app it watches, and grows buckets with zero friction.
- **Spine from shape:** observe-only · outlives-the-app · bounded storage · additive buckets ·
  owner-only · probes-stay-home.

## Significance

**Significant.** It forces three decisions: a new **service/process**, a new **store boundary**,
and a new **plugin boundary**. So the lenses get walked.

## Grounded in (real code)

- Deploy is Docker Compose + Caddy + blue-green on OCI: `services/go-api/deploy/compose.prod.yml`,
  `deploy/Caddyfile`, `deploy/blue-green.sh`, `deploy/Dockerfile`.
- go-api is one Go binary, config-driven: `services/go-api/cmd/api/main.go:1-40`.
- Hexagonal layering (domain, application, adapters) is the project's law.
- Events leave the process **only over HTTP/SSE**: client `/v1/events` behind `auth.Middleware`
  (`internal/app/routes.go:66,46`), operator `/admin/events/stream`
  (`internal/admin/handler/events_handler.go:17-25`), generic streamer
  (`internal/admin/handler/sse.go:10-41`).
- The in-process tap is **single-subscriber** — a second process cannot attach:
  `internal/admin/eventtap/tap.go:61-78` (`SubscribeAll` errors if already subscribed).
- Auth surfaces: `/health` open, `/v1/*` JWT (`routes.go:45-46`), `/admin/*` operator-only for
  writes and operator-or-read-only for GETs.

## Design decisions (lens by lens)

### Boundaries — where it lives

A new Go service **`services/overseer/`** in the monorepo, its own process/container. It observes
go-api **only across go-api's public HTTP surface** (SSE streams + REST + `/health`),
authenticating as go-api's read-only admin principal. It never imports go-api's internal runtime
packages; it may share read-only DTO/event *type* definitions (a small shared module) purely to
decode.

- **Over:** in-process (like Mission Control) — rejected, fails "outlives-the-app" at the root.
- **Over:** importing go-api internal packages for data — rejected, couples build and runtime and
  quietly rebuilds the in-process coupling we're escaping.

### Data & state — the store

**First slice: in-memory bounded ring buffers only.** Live activity is minutes-scoped (a live
event stream + in-flight requests), so it needs no database. Define a `Store` interface now so a
persistent impl slots in later without touching buckets.

When history buckets arrive (Reliability, Perf), Overseer **owns its own logical database on the
existing Postgres server** (separate DB, Overseer-owned), reusing the pgx stack.

- **Over:** its own Postgres *instance* — rejected, overhead for one operator; process-outlives-
  process is met without it. Residual risk: a shared-instance outage blinds both; acceptable for
  v1, and the off-box uptime check remains the total-down backstop.
- **Over:** writing into go-api's database — rejected, violates ownership and couples schemas.

### Coupling — the plugin model

The shell defines a **`Bucket` interface (Collect / Store / Render)** plus a **registry**. Buckets
self-register at startup; the shell core references **no concrete bucket**. A bucket owns its own
files plus exactly one registration line. This is the additive-buckets invariant made real, and
the direct fix for what killed Mission Control (buckets hard-wired into one file).

### Scaling / hot paths

Single user, low volume — not a scaling problem. The one real concern is the **event consumer
surviving go-api restarts**: reconnect with backoff, resume the stream. Ring buffers cap memory by
construction. No hot path.

### Failure / degradation

The "outlives-the-app" invariant, realized: when go-api is unreachable, each bucket serves its
**last-known state flagged stale**, plus an explicit **"source down"** signal; the shell never
crashes because one bucket's source is down. SSE consumers reconnect with backoff.

### Infra / tech-stack fit

Go, matching go-api: reuse chi, slog, config, pgx, jwx, and the deploy pattern. Overseer serves an
**embedded web UI** structured for plugin panels (each bucket contributes a panel); the visual
theme stays deferred per shape. Deployment: a **new service in `compose.prod.yml` behind Caddy**,
its own container, so it survives go-api restarts; the existing off-box uptime check stays as the
"everything is down" backstop.

- **Over:** a separate React/Expo web app now — rejected, don't build a second frontend before the
  theme matters; the embedded UI is enough to prove the platform.

## Architectural invariants (add to the spine)

- Overseer **never imports go-api internal runtime packages**; all app data crosses via go-api's
  public HTTP surface.
- Overseer authenticates to go-api as a **read-only principal holding no write scope** — a Supabase
  user that is not `OPERATOR_USER_ID`, which go-api's admin gate admits on GET and answers 403 on
  every mutating admin route (#1810). Observe-only is enforced at the server, not only by the
  client never calling a write; the client-side half remains as defence in depth.
- Every bucket is a **plugin implementing Collect/Store/Render**; the shell core references no
  concrete bucket.
- When a bucket's source is unreachable, the bucket **serves last-known state flagged stale** and
  the shell stays up.
- Overseer **owns its own store** and never reads or writes go-api's database.

## Slice-1 build (what ticketize cuts first)

1. `services/overseer/` scaffold: Go service, config, slog, embedded-UI shell, deploy wiring
   (compose.prod.yml + Caddy), health endpoint.
2. Plugin core: `Bucket` interface + registry + in-memory bounded `Store` interface.
3. Live activity bucket: consumes go-api's event SSE (operator auth) + in-flight requests, bounded
   ring buffers, renders its panel. Reconnect-with-backoff on go-api restart.
4. A stub second bucket proving registration touches only its own files + one line.

Then, in order: metrics-exposure enabler (unblocks Perf) → Reliability → Perf → remaining buckets
→ remove Mission Control.
