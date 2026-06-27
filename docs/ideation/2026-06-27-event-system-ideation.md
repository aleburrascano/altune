---
date: 2026-06-27
topic: event-system
focus: extend the behavioral telemetry event system to improve discovery ranking; clarify telemetry vs events folders
mode: repo-grounded
---

# Ideation: Pushing the Event / Observability System to Its Limits

## Grounding Context (Codebase)

**Current event system (post-unification, this branch `feat/discovery-tail-noise-demotion`):**
- Unified `InteractionEvent` envelope → Postgres `discovery_events` (`user_id`, `event_type`, `query_norm` nullable, `payload` JSONB, `occurred_at`). `POST /v1/discovery/events`.
- 7 event types: `search_performed`, `result_clicked`, `play`, `skip`, `completed`, `library_add`, `wrong_album`.
- Frontend split: `apps/mobile/src/shared/telemetry/` (`useRecordEvent`, **outbound** fire-and-forget emission) vs `apps/mobile/src/shared/events/` (`useServerEvents` + `sse-client.ts`, **inbound** SSE for domain/cache sync). **Different directions — keep separate.**
- Consumed today: `search_performed.zero_result` → `ZeroResultQueries`; `result_clicked` → `NonZeroNoClickQueries`. Both feed coverage signals / eval.
- **Recorded but UNCONSUMED:** `play`, `skip`, `completed` (emitting since commit 0b0e45c), `wrong_album` (type exists, no UI emitter).
- Ranking = RRF + token similarity + multi-source bonus (`service/rank.go`); eval `cmd/discoveryeval`; popularity signal reverted (regressed hard corpus 81%→75% — niche library).
- Settled design rules: async best-effort emission; JSONB payload grows without migration ("collect richly, model lazily"); failures logged-not-swallowed; SSE bus is lossy per-user by design (ADR-0012); Mission Control is a self-built console (Grafana/OTel/Sentry rejected), in-memory counters that reset on restart.

**Key gaps the ideation attacks:** no `search_id`/`query_id` join key; no impression logging (log clicks, never what was *shown*); `result_clicked` carries no `position`/`confidence`/`provider`; no `session_id`; `play` fires on start (not a listen threshold), no dwell; no reformulation/abandonment tracking; play/skip/completed unconsumed; Mission Control amnesiac; alerting unbuilt.

**External grounding:** Azure AI Search (`query_id` as minimal join key); Promoted.ai/IAB (visibility-confirmed impressions are the prerequisite for any position-bias correction); Kim WSDM 2014 (dwell <20–30s = dissatisfaction, >120s = satisfaction); Joachims query-chains (implicit feedback → free LTR labels; reformulation = dissatisfaction); bandit-style randomization as the small-scale substitute for IPS. Music signal hierarchy: save > play-to-completion + repeat > playlist-add > click→play > pogo-stick (neg) > skip<30s (neg) > raw click (noisy).

## Ranked Ideas

### 1. The `search_id` keystone + rich impression/click capture  *(trunk)*
**Description:** Mint a UUID per `search_performed`, return it in the search response, require it on every downstream engage event. Log a `results_shown` impression event (ordered signatures + position + provider + confidence). Add `position`/`confidence`/`provider`/`result_signature` to the `result_clicked` payload (data is already at the call-site).
**Warrant:** `direct:` clicks join by lossy `query_norm` string; `result_clicked` drops position/confidence/provider; no impression log. `external:` Azure AI Search query_id; Promoted.ai/IAB impressions.
**Rationale:** Every show-conditioned metric (CTR@position, MRR/NDCG, counterfactual eval) is uncomputable without this and irrecoverable for prior history. All 6 frames converged here.
**Downsides:** impression volume (use one summary event/search); RN viewport visibility via `onViewableItemsChanged`.
**Confidence:** 95% · **Complexity:** Low–Medium · **Status:** Explored

### 2. Activate dormant signals — consume play/skip/completed with a listen threshold
**Description:** Fire the satisfaction-play at 30s/50%-duration; capture listen-duration/dwell on skip & completed; build the consumer (skip-after-click = negative, play-to-completion = positive).
**Warrant:** `direct:` these three "recorded but NOT consumed — collection sunk." `external:` Kim WSDM 2014 dwell.
**Rationale:** Highest ROI — instrumentation + months of history already paid. Skip-after-click catches bait-and-switch titles raw CTR over-rewards.
**Downsides:** expo-audio progress hook; skip confounded until paired with session (#5).
**Confidence:** 90% · **Complexity:** Low–Medium · **Status:** Explored

### 3. Self-growing eval corpus from behavioral labels  *(flywheel)*
**Description:** search→complete→`library_add` = free positive label; `wrong_album` = free hard negative. Materialize nightly into the `cmd/discoveryeval` corpus format.
**Warrant:** `direct:` grounding names auto-generated corpus. `external:` Joachims query-chains.
**Rationale:** Structurally solves the niche-library eval problem (labels about *your* catalog/tastes); sharpens daily. The in-sample answer to why popularity failed.
**Downsides:** needs #1; cold-start; must stay same-sample-gated vs the human corpus.
**Confidence:** 85% · **Complexity:** Medium · **Status:** Explored

### 4. Offline counterfactual replay + exploration randomization
**Description:** Replay a candidate ranker over historical impression+label logs to score it without serving it; add ~2–5% randomized-order responses (logged) for unbiased propensity data. Optionally a CI eval gate on ranking diffs.
**Warrant:** `external:` Joachims IPS + Promoted.ai offline eval; bandit randomization at small scale. `direct:` tail-noise demotion flag is default-off pending exactly this A/B gate.
**Rationale:** Collapses experiment cost from weeks-shipped-dark to a same-day offline run; settles the tail-noise flag from data already collected.
**Downsides:** noisy at family volume; exploration degrades order for a few searches (needs sign-off).
**Confidence:** 75% · **Complexity:** Medium–High · **Status:** Explored

### 5. Sessionize — arc as the unit; reformulation/abandonment as negatives
**Description:** Attach `session_id`; treat search→click→play as the record. Derive (SQL-side) `abandoned_search` (no-engagement search + another within 60s) and pogo-sticking.
**Warrant:** `direct:` no session id today. `external:` ASRS near-miss principle + Joachims query-chains.
**Rationale:** Reformulation reveals what the user *meant*, informing token-similarity/query-norm; retargets from "good clicks" to "satisfied sessions."
**Downsides:** session-boundary policy is a judgment call; depends on #1.
**Confidence:** 80% · **Complexity:** Medium · **Status:** Explored

### 6. Two-tier reliability — client outbox for label-critical events
**Description:** `library_add`/`wrong_album` get a client-side outbox + idempotency key (retry, dedup); everything else stays fire-and-forget. Add dual timestamps (`client_occurred_at` + server `received_at`).
**Warrant:** `direct:` a lost save = a lost label + library-state bug; no dedup key today. `external:` exactly-once for relevance-critical, best-effort for passive.
**Rationale:** Protects the highest-value signals without durability cost on the 90% that don't matter; idempotency makes retry safe.
**Downsides:** mobile outbox complexity — only for the critical tier.
**Confidence:** 80% · **Complexity:** Medium · **Status:** Explored

### 7. Mission Control hardening — persisted history + coverage alerting
**Description:** Nightly rollup table (`discovery_metrics`: per-query CTR, zero-result rate, `tail_noise_top5`) for week-over-week history; wire `ZeroResultQueries`/`NonZeroNoClickQueries` into threshold alerts.
**Warrant:** `direct:` in-memory counters reset on restart; coverage signals computed-but-unwatched; alerting unbuilt.
**Rationale:** Console stops being amnesiac; blind spots page you instead of waiting to be noticed; promotes the exported tail-noise metric to a tracked gauge.
**Downsides:** don't ride the lossy SSE bus (ADR-0012); keep `user_id` out of metrics (cardinality/privacy).
**Confidence:** 80% · **Complexity:** Low–Medium · **Status:** Explored

### Cross-cutting seam
An `EventConsumer` port that #2/#3/#5 plug into → each new signal is "add an implementation," not "rewire the pipeline" (the Strategy/Observer shape the repo's rules endorse).

## Rejection Summary

| # | Idea | Reason Rejected |
|---|------|-----------------|
| 1 | Per-user / personalized relevance from events | Strong & grounded, but a ranking-strategy topic — separate brainstorm; enabled by #1+#3 |
| 2 | Closed-loop self-healing / nightly auto-tuning ranker | Premature; it's #1–#4 composed. Revisit as ce-optimize later |
| 3 | Events as durable read-back log ("on this day", history) | Product feature, not discovery improvement — out of scope |
| 4 | Drop family-scale anonymity / full attribution | A decision not a build; folds into per-user |
| 5 | Self-pruning JSONB payloads | YAGNI at 7 event types |
| 6 | Mark-recapture / expected-goals / slippage / sentinel-queries metrics | Clever analysis methods, all downstream of #1+#2 existing |
| 7 | Schema-version + typed payload registry | Folded into #1 as the disciplined way to add fields |
| 8 | CI eval-gate on ranking diffs | Folded into #4 (same eval substrate) |
