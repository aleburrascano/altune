# Overseer UI — split into a JSON API + a real web app

The deferred "visual theme" slice, now taken. Prior docs (inherited, not re-argued):
`docs/drafts/overseer.md` (what), `docs/drafts/overseer-design.md` (how),
`docs/features/overseer/notes.md` (what shipped). This brief settles the *what* of the UI
reframe; architecture is `design`'s job.

## Vision, what it's for: the problem, the user, the outcome (the goal + why)

Overseer works but nobody wants to open it. It's a plain wall of dark cards, rendered once on
page load, and **each bucket hand-writes its own HTML** — so it can never be made cohesive and a
restyle is 8× work forever. The "god's-eye control room" the vision promised is unrealized as a
*surface*: the data is there, the experience is not.

- **User:** the owner. One person. Forever. (Unchanged from the platform spine.)
- **Problem:** no single surface makes the whole app's health legible *and* pleasant to read; the
  current one is static, ugly, and structurally un-themeable.
- **Outcome:** a control room the owner actually *wants* to open — readable at a glance, navigable
  without hunting, live without a manual refresh, and worth visiting because it tells you something
  true about the system. Built for operational value, not for fun.

## The idea, the proposal itself, the reasoning, and the alternatives weighed and rejected

**Split Overseer into two concerns.** The Go service becomes a **pure JSON API**: each bucket
exposes *structured data* (facts — numbers, statuses, rows, time-series points), never HTML. A new
**React + Vite + TypeScript web app** owns all display, and is **served by the Go binary itself**
(`go:embed` static files, same container behind Caddy).

- **Structured data** = the bucket hands over the spreadsheet, not the printed chart. The frontend
  decides how to draw it, so one design system covers every panel and a restyle lives in one place.
- **Live in the browser** via **SSE over a fetch-based stream reader** (not native `EventSource`,
  which can't send an auth header). The bearer token rides along; panels update in real time.
- **Auth: sign in with your real Supabase account.** Overseer allowlists exactly one user id /
  operator role and rejects every other authenticated user. Session is a bearer token held in
  memory + refresh. **No cookie.**
- **Additive on both sides.** A frontend **panel registry keyed by bucket id** mirrors the Go
  registry. A new bucket = its backend files + 1 registration line AND one panel component + 1
  registry line. Neither core references a concrete bucket.
- **Navigation:** an overview (all buckets glanceable) + drill-down, not a flat grid of 8 cards.
- **Feel:** Vercel + Linear + Grafana — clean, sharp, dense-but-legible.

**Alternatives weighed and rejected:**

- *Keep the embedded Go HTML rendering, just theme the shell.* Rejected: each bucket emits raw
  HTML, so cohesion is impossible and every restyle is 8× work. This is the root cause, not a skin.
- *Move buckets to structured data but keep server-rendering in Go.* Rejected: solves cohesion but
  not the experience — no real component system, no live client, no navigation. Separation of
  concerns is the cleaner cut.
- *Expo web, reuse the mobile stack (Expo 57 + react-native-web is present).* Rejected: a dense
  desktop ops dashboard fights RN primitives; Vercel/Linear/Grafana are all React web.
- *Host the SPA separately (Vercel-style static host).* Rejected: adds CORS, a second thing to keep
  alive, and blinds you if that host is down. Serving from the overseer binary keeps "outlives the
  app" and same-origin auth for free.
- *Owner-token cookie (the shipped mechanism).* Rejected by the owner. Replaced with Supabase login.
- *RBAC / roles.* Rejected: contradicts the owner-only invariant; roles are machinery for many
  users. One allowlisted user id is the access control. A single operator-role claim leaves the door
  open to real roles later *if* control actions ever arrive — without building the framework now.

## Assumptions, the unstated beliefs it rests on; mark the load-bearing ones

- **[load-bearing]** Exactly one user, forever. This is what makes RBAC wrong and a single-id
  allowlist right.
- **[load-bearing]** The SPA is served by the overseer binary → **same-origin**, so bearer + a
  fetch-stream live channel work with no CORS.
- **[load-bearing]** Supabase stays the identity provider and Overseer can validate an owner token /
  operator claim (it already exchanges Supabase refresh tokens for its go-api operator principal).
- **[load-bearing]** Every existing spine invariant survives the rewrite (observe-only,
  outlives-the-app, bounded storage, additive buckets, owner-only, probes-stay-home,
  degrade-don't-crash, no-internal-imports). The rewrite must not quietly drop one.
- Rewriting all 8 buckets from `Render() Panel` to a structured-data contract is an acceptable
  one-time cost.

## Scope / non-goals, what's in, and explicitly what's OUT

**In scope:**

- **Go API:** replace `core.Bucket.Render() Panel` with a **structured-data contract** per bucket;
  JSON endpoint(s) per bucket; an **SSE live channel** (fetch-stream, bearer-authed); **Supabase
  login + owner allowlist** replacing the cookie/owner-token path.
- **Frontend:** a new React + Vite + TS app in the repo; a **panel registry** keyed by bucket id;
  **overview + drill-down** navigation; the **Vercel/Linear/Grafana design system**; every panel
  renders the three states — **live · stale · source-down**; embedded + served by the Go binary.
- **All 8 existing buckets ported** to the data contract + a frontend panel each (live activity,
  reliability, backend perf, domain quality, usage, cost, security, logs).

**Out of scope / non-goals:**

- **Control / actions.** Still observe-only. No restart, no kill switches.
- **Multi-user, sharing, public access, RBAC.** Single owner.
- **New buckets or new signals** beyond the 8 that exist. This is a re-surfacing, not new coverage.
- **A native/mobile Overseer.** Web only.
- **A separate static host / Vercel deploy.** The binary serves the SPA.
- **Removing Mission Control.** Separate, already-tracked ticket.
- **Renaming.** "Overseer" stays.

## Priority, must-have vs nice-to-have, separated

**Must-have — the walking skeleton (open the site, log in, see one bucket live):**

- The split proven end to end on **one bucket, Live activity**: Go exposes it as JSON + SSE; the SPA
  logs in with Supabase; renders it updating in real time; served by the overseer binary.
- **Auth:** Supabase login + owner allowlist, bearer in memory + refresh, no cookie.
- **The design-system shell** (navigation frame + one fully themed panel) so the look is *set*, not
  retrofitted after 8 panels exist.
- **The frontend panel registry**, proven by a second (stub) panel that registers without touching
  the core.

**Then (each its own slice):**

- Port the remaining 7 buckets to data + panels.
- The **overview page** (all buckets glanceable) + drill-down.
- Retire the embedded Go HTML rendering and the old cookie/owner-token login once parity is reached.

**Nice-to-have (later):** motion / micro-interactions, keyboard navigation, richer empty states.

## Must-holds, the feature's core rules

Known now, each stated so a test could check it. Living — grows as the build reveals more.

- **Single owner only.** A valid *non-owner* Supabase token is rejected (403). Only the allowlisted
  user id / operator claim passes. No RBAC.
- **No cookie.** Auth is bearer only; no overseer route emits `Set-Cookie`.
- **Live channel is authed.** The SSE/fetch-stream rejects an unauthenticated or non-owner reader.
- **Observe-only, preserved.** The JSON API exposes no mutating route; the go-api client stays
  read-only (existing reflection guard extends to the new surface).
- **Outlives the app.** Overseer up ⇒ the site loads; when go-api is down, every panel renders
  **last-known state + source-down**, never a blank or a crash.
- **Additive on both sides.** A new bucket touches only its own backend files + 1 line AND one
  frontend panel + 1 line; neither core references a concrete bucket. (Guard test each side.)
- **Three states per panel.** Every panel renders **live**, **stale**, and **source-down** cleanly.
- **Carried forward unbroken:** bounded storage, degrade-don't-crash (panic contained on
  collect/store/render), no go-api internal imports, and output-escaping of watched-app text.

## Open questions, the parked unknowns, for the readiness gate

None block `design`. The following are **design's calls**, defaulted here so the lane keeps moving;
design confirms against the real code:

- **Typed contract sync (default: hand-write TS types to match the Go JSON).** Revisit codegen
  (OpenAPI or similar) only if the types drift in practice. One dev, keep it light.
- **Supabase token validation (default: verify the JWT locally via JWKS).** Avoids a Supabase
  round-trip per request; design confirms the claim/allowlist shape.
- **Component base (default: Tailwind + a headless kit like Radix/shadcn).** Speeds the
  Vercel/Linear/Grafana look; design confirms the token set.
- **Overview "glanceable" content per bucket:** deferred to when buckets are ported — decided
  panel-by-panel, not up front.
