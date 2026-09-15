# Overseer (working name)

Idea dump for shaping. Weigh verdict: `docs/drafts/overseer-weigh.md` (worth it, build fresh,
separate from the app it watches). This dump settles the *what*; architecture is `design`'s job.

## Vision

A god's-eye control room for **one user** — the owner. You open it and see the whole app at
once, and you should feel like you *are* the platform: total awareness, nothing hidden.

The problem it solves: today there is no single place that shows the whole app's state. The
existing operator dashboard ("Mission Control", in-process in go-api) failed this — the owner
never opens it, it looks poor, it only mirrors backend ops, and it dies when the app dies. The
outcome wanted: one surface the owner actually *wants* to open, that spans the entire app
(front-end, back-end, security, domain, usage, cost), keeps working when the app is down, and
gains new coverage over time without friction.

Why it matters now: the app is a music streamer meant to feel Spotify-grade. Latency and
quality are features, not afterthoughts, and there is currently no lens that makes the whole
system's health legible to its owner.

## The idea

A standalone monitoring/analytics platform, the "Overseer", that watches the entire Altune app
and presents it as a set of **buckets** — one area of awareness each. Buckets are added over
time; the platform is built so a new bucket slots in without disturbing the existing ones.

Eight buckets are named up front (so the whole thing is designed against the full set, not just
the first). Each is a container that grows its *own* signals over time — "record live activity
better" in two months is a new signal inside a bucket, not a rebuild. An empty signal is a
*visible* gap, which is how the owner spots missing coverage.

The eight buckets:

1. **Live activity** — real-time events + requests in flight.
2. **Reliability** — uptime, health, errors, alerts.
3. **Back-end performance** — latency across layers, throughput. High priority (streaming app).
4. **Front-end health** — looks, load, UX, visual regressions (an active front-end prober).
5. **Security** — break-in probes, vuln scans, auth anomalies (an active back-end prober).
6. **Domain quality** — discovery/search quality, catalog, acquisition. Deepest business logic.
7. **Usage** — what the owner actually does in the app.
8. **Cost** — infra (OCI) + provider API spend.

**Alternatives weighed and rejected:**

- *Fix/extend Mission Control instead.* Rejected: reusing its guts would drag its failed
  vision forward ("Mission Control V2"), and its in-process design fails the "outlives the app"
  requirement at the root. The dashboard itself is only ~727 lines — little worth saving.
- *Build the two probers + a big platform all up front.* Rejected: that is the "everything up
  front, then it's sloppy" trap. Build incrementally so cracks show early.
- *A fully separate project/repo.* Rejected at the product level as overhead for one operator;
  the process-vs-repo split is design's call, but the intent is "its own thing, not its own
  universe."

## Assumptions

- **[load-bearing]** New buckets will keep being added for the life of the project. The whole
  shape lives or dies on adding a bucket being frictionless.
- **[load-bearing]** The owner is the only user, ever. No multi-tenant, no sharing, no public.
- **[load-bearing]** The platform must keep working, and stay reachable, when the watched app is
  down — that is the whole point of "outlives the app".
- The active probers (front-end, security) only ever target the owner's own infra. This is
  authorized self-testing, not testing of anyone else.
- Passive data collection is cheap enough to run continuously; only heavy active probes need to
  be scheduled.

## Scope / non-goals

**In scope:**

- The platform itself: a shell that hosts buckets, and the discipline that lets buckets be added
  without touching existing ones.
- All eight buckets *as named slots*. Only Live activity is specified deeply in this pass.
- The first slice built end to end: the shell + Live activity (see Priority).

**Out of scope / non-goals:**

- **Control.** The Overseer *observes only*. It never acts on the app (no restart, no flipping
  kill switches, no triggering reacquire). Control is a deliberate later decision at a different
  trust level, not v1.
- **Multi-user / sharing / public access.** Single owner, forever.
- **Deep specs for buckets 2–8.** Each gets its own shape pass when its turn comes. Named here,
  not designed here.
- **The name.** "Overseer" is a placeholder; the real name is a later decision.
- **Visual theme / design language.** Deferred; comes once the platform proves itself.
- **Deleting Mission Control now.** It stays running until the Overseer surpasses it, then it is
  removed in its own ticket — no blind window in between.

## Priority

**Must-have (the first slice):**

- The platform shell: something that hosts buckets and lets a new one register without changing
  the core.
- Exactly one real bucket, **Live activity**, end to end. v1 shows a **live event stream** plus a
  **list/count of requests in flight**, both bounded.
- Proof the shape holds: a second (stub) bucket can register without touching the core.

**Then (each its own slice, in rough order):**

- The enabler that unblocks Back-end performance: exposing metrics + timing across layers (today
  counters increment into `expvar` but nothing serves them, and nothing times across layers).
- Reliability bucket (data already exists to feed it).
- Back-end performance bucket (after the enabler).
- The remaining buckets: Domain quality, Usage, Front-end health, Security, Cost.
- Remove Mission Control.

**Nice-to-have (later):** the real name, the visual theme.

## Invariants

The spine — known now, stated so a test could check them. Grows as the build reveals more.

- **Observe-only:** the Overseer has no path that mutates the watched app. No write/command call
  to go-api exists in the platform. (A test asserts the app-facing client is read-only.)
- **Outlives the app:** when the watched app is fully down, the Overseer is still up and still
  serves its last-known state + the "app is down" signal. (Kill the app; the Overseer still
  responds.)
- **Bounded storage, always:** every bucket persists within a fixed bound — a ring or a windowed
  rollup. No bucket can grow storage without limit. (A test feeds N×capacity and asserts size
  stays capped.)
- **Additive buckets:** registering a new bucket requires no change to any existing bucket's
  code. (Adding the stub bucket touches only its own files + a registration point.)
- **Owner-only:** no unauthenticated or non-owner request ever reaches Overseer data. (An
  unauthenticated request is rejected.)
- **Probes stay home:** active probers only ever target the owner's own infra; no probe can be
  pointed at an external target. (Probe targets are validated against an allowlist of own infra.)

## Open questions

Parked. None of these block designing the platform spine or the Live activity first slice; they
belong to future bucket passes.

- **Active-probe scheduling (buckets 4 & 5):** how "quiet hours" for heavy probes (load tests,
  break-in) are decided and triggered. Owner flagged this needs more exploration. Deferred to the
  shape pass for those buckets.
- **The name** (deferred by choice).
- **Visual theme** (deferred by choice).
