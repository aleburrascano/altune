# Capability: Overseer UI revamp — reliable connection, history, rebuilt UI

Epic #2348 (closed). Plan: `docs/features/overseer-revamp/plan.md`. Carries forward
`docs/features/overseer/notes.md` (the platform spine) and `docs/features/overseer-ui/notes.md`
(the React SPA split) — spine unchanged: observe-only, outlives the app, bounded storage,
additive buckets, owner-only, degrade don't crash, no go-api internal imports.

## What now works

The owner opens Overseer and sees a dense, dark control room, not a wall of text, with real
charts over history that survives a restart, one shared panel/chart kit across every bucket, and
an honest live-status pill.

- **A connection to go-api that stays up.** The read-only operator account re-signs in on a spent
  (400) refresh token instead of needing a hand-seeded token; a still-valid access token keeps
  being used while a refresh backs off; failures are sorted into `auth` / `throttled` / `degraded`
  / `down` so "stale" always says why; Overseer never sends more than one retry per request after
  a 401 (can't trip go-api's throttle); streams go `connecting` on a drop and reconnect on their
  own after ~60s silent; domainquality's three go-api reads run in parallel with their own
  deadlines; prod/staging read go-api over the internal Caddy listener (`:8081` prod, `:8082`
  staging) so the blue/green flip is transparent to Overseer.
- **History that survives restarts.** A disk-backed SQLite store (`modernc.org/sqlite`, no cgo) on
  the existing `overseer-data` volume (`/var/lib/overseer/history.db`) keeps raw points 24h and
  1-minute rollups 7d, capped per series, pruned on a timer. Rings restore from disk on boot.
  `GET /api/buckets/{id}/series?range=1h|24h|7d` serves the chart data; snapshots carry a `spark`
  field for overview tiles; `/health` data (last cycle, credential state) is exposed the same way.
- **A rebuilt dark UI.** Tailwind + Radix panel kit (`Panel`, `Section`, `Metric`/`StatGrid`,
  `DataTable`, `SignalList`, `Notice`, `StateBadge`, `RelativeTime`) and a single uPlot chart set
  (`Sparkline`, `TimeSeries`, `BarSeries`, `UptimeStrip`) replace every hand-styled panel across
  all 8 buckets plus `heartbeat`; no inline `style={}`, no `dangerouslySetInnerHTML` (lint-enforced).
  Responsive app shell (side nav on desktop, drawer on phone, no sideways scroll at 360px),
  command palette (Cmd/Ctrl-K), time-range picker kept in the URL, correlation-id cross-linking,
  search/filter for logs and usage, keyboard-reachable nav with visible focus.
- **Honest live status.** Any stream frame received after an error sets the connection pill back
  to live; no frame for ~3 ticks shows stalled, never live.
- **Contract safety.** A test pins the Go snapshot/series JSON to `web/src/types.ts` so the two
  sides can't silently drift.

## How to reach it

- Prod: `https://altune.duckdns.org/overseer/` — owner sign-in (Supabase), same as before the
  revamp. `GET /overseer/config.json` is open and public (prod Supabase project). The guarded API
  (`/overseer/api/buckets`, `/overseer/api/health`, `/overseer/api/stream`,
  `/overseer/api/buckets/{id}/series`) returns 401 with no bearer, 403 for a non-owner.
- Staging: same shape, on `:8082` internally.
- Local dev: `cd services/overseer/web && npm install && npm run build` (embeds `dist/` into the
  Go binary), then `cd services/overseer && go run ./cmd/overseer`.
- History file lives on the `overseer-data` volume at `/var/lib/overseer/history.db`; a missing or
  corrupt file starts Overseer empty rather than failing to boot.

## Must-holds it keeps

Carried from the plan/epic, unchanged targets for the next change to Overseer:

- Every series stays under its row cap; points older than 7d are gone after a prune run.
- A missing/corrupt history file never stops Overseer booting — starts empty, says so.
- `/api/buckets`, `/api/buckets/{id}/series`, `/api/stream`, and guarded health data all return 401
  with no bearer, 403 for a non-owner.
- A stream frame after an error puts the pill back to live; ~3 silent ticks show stalled, never live.
- No page scrolls sideways at 360px; no inline `style={}`; no `dangerouslySetInnerHTML` (lint).
- Go snapshot/series JSON stays pinned to `web/src/types.ts` (contract test).
- Every nav item and control is keyboard-reachable with visible focus.
- Overseer reads the live blue/green color with no restart after a flip or rollback.
- The password grant runs at most once per backoff window and never logs the password.
- A spent refresh token (400) leads to a fresh sign-in with no human step; a failed credential
  shows as `auth`, never as go-api being down; a 503-with-body shows as `degraded` with the real
  dependency, not stale.
- A stream drop shows `connecting`; a stream silent ~60s reconnects on its own.
- At most one retry per request after a 401 (can't trip go-api's auth throttle).
- Gzipped JS bundle stays under 250 KB (CI-enforced).
- Two processes sharing one refresh-token file never exchange the same token.
- `internal/history` never imports a concrete bucket; core still names no concrete bucket.

## Where it runs

Same single-container deploy as the platform spine: `altune-overseer` on the OCI VM behind Caddy
at `/overseer/*`, no new service. This epic added:

- The disk history store on the existing `overseer-data` volume.
- Two new internal Caddy listeners, `:8081` (prod) and `:8082` (staging), that the go-api
  operator/read-only principal reads through instead of going out through DuckDNS and back in.
  Caddy was recreated to add them.
- Two new read-only-principal secrets per environment: `OVERSEER_GOAPI_READONLY_EMAIL` /
  `_PASSWORD` (the self-healing credential's sign-in), plus `OPERATOR_READONLY_USER_ID` and an
  `OVERSEER_GOAPI_URL` pointed at the internal Caddy port. Read-only Supabase accounts now exist
  in both the prod and staging Supabase projects.
- The OCI usage-api IAM grant for the cost bucket, verified working via the instance principal.

Deploy for this release was manual, not the CI path: b41945e0 was pushed to prod over SSH running
the same scripts CI uses (`prod-migrate.sh`, `blue-green.sh`, `overseer.sh`), because the
`deploy-prod` job was stuck on a stale concurrency lock. PR #2587 fixes that lock so the next
release deploys through CI normally.

## Verified live at close

- `https://altune.duckdns.org/overseer/` → 200; `config.json` carries the prod Supabase project.
- Guarded routes (`buckets`, `health`, `stream`) → 401 with no bearer.
- `/var/lib/overseer/history.db` exists on prod (history survives restart — scenario S4).
- The read-only operator principal (#1810 account) reads go-api admin routes through the internal
  Caddy listener `http://altune-caddy:8081` — all 200.
- The go-api blue-to-green flip was clean.
- Staging is the same shape, on the internal `:8082` listener.

## What broke before / not walked

- Prior state (pre-epic): the connection to go-api had been dead for days on a spent single-use
  refresh token, and history reset on every deploy — both are what this epic fixed; see "What now
  works" above.
- **Not walked by an agent, needs a signed-in owner in a browser:** S1 (fresh sign-in lands on the
  new shell with a working nav and honest live pill), S3 (Reliability detail page renders an uptime
  and latency chart for 1h/24h/7d from the disk store), S6 (phone layout, no sideways scroll), S7
  (command palette / keyboard nav reachability). Only S4 (history kept across a restart) and S5
  (the 401 leg) were machine-proved. The must-holds these scenarios cover are unchanged risk until
  a human owner walks them once in a browser; nothing in ship's or qa's evidence contradicts them,
  they are simply unconfirmed.
