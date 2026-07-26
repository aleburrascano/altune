---
type: Subsystem
title: Acquisition retry admission
description: RetryAdmission's whole-policy gate for manual re-acquisition — failed-state-only, per-track cooldown — and the handler that maps its outcomes to transport codes.
resource: services/go-api/internal/acquisition/service/retry_admission.go, services/go-api/internal/acquisition/adapters/handler/retry_handler.go
tags: [acquisition, retry, admission-policy, go-api]
verified_commit: b1b3e3867ff5d3319beb9b3d361d8625cea3ec94
---

`RetryAdmission` (`retry_admission.go`) owns the entire admission policy for a manual re-acquisition retry, not just the cooldown: `Admit(track)` first rejects any track not in `AcquisitionFailed` state (`ErrRetryNotFailed`), then rejects a retry admitted less than `RetryCooldown` (60s) ago for that same track (`ErrRetryCooldown`). Both checks live service-side deliberately — the failed-state check used to live in the HTTP handler, and keeping the whole policy in one place means a second retry entry point (another handler, a CLI command) cannot accidentally admit a track the first entry point would have rejected by only replicating half the logic.

State is a single `map[string]time.Time` (track id → last admitted retry) behind a mutex, not a TTL cache or scheduled sweep: `Admit` opportunistically prunes any entry older than `2*cooldown` on every call, so the map only grows with actual retry traffic and never needs a background goroutine. A successful `Admit` records `now` for the track before returning nil — the record happens on admission, not on the retry's eventual completion, so a second retry request arriving mid-flight is still correctly rate-limited even though the first hasn't resolved yet.

`RetryHandler` (`adapters/handler/retry_handler.go`) is a thin translation layer: retries carry no source URL — the request is by `trackId`, so acquisition re-searches by metadata; it loads the track, calls `admission.Admit`, and maps `ErrRetryNotFailed` → 409 Conflict and `ErrRetryCooldown` → 429 Too Many Requests. On admission it calls `scheduler.Schedule(userId, trackId, "")` — retries carry no source URL (the request is keyed by `trackId`, not a pasted link), so acquisition always falls back to the full search pipeline (see [pipeline](pipeline.md)) rather than retrying a specific candidate URL. The handler holds its own `*service.RetryAdmission` instance (constructed in `NewRetryHandler`), so admission state is scoped to the handler's lifetime — there is exactly one retry entry point in the app today, wired at composition-root time (see [app-wiring](../app-wiring.md)).

## Two admission policies (2026-07-26)

Manual re-acquisition now has two entry points with opposite preconditions, so the cooldown mechanics were extracted into an unexported `cooldownGate` (a mutex, a duration, a `map[string]time.Time`, opportunistic pruning at `2*cooldown`) that both wrap. Each policy still owns its own state check, which is the point of keeping the whole policy service-side.

`RetryAdmission` is unchanged: `AcquisitionFailed` only (`ErrRetryNotFailed` → 409), one per track per 60s. `ReacquireAdmission` is its mirror for the opposite case — it requires `track.IsStreamable()`, i.e. ready *with* an audio ref (`ErrReacquireNotReady` → 409), because replacing audio that does not exist is retry's job. Both surface `ErrRetryCooldown` → 429. Each holds its own gate, so a retry and a re-acquire on the same track do not share a cooldown; they are different operations with different risk, and rate-limiting them together would be arbitrary.
