---
type: Index
title: Acquisition subsystems
description: The acquisition bounded context decomposed into its three machines — the audio-finding pipeline, background job scheduling, and manual-retry admission.
tags: [index, acquisition]
verified_commit: b1b3e3867ff5d3319beb9b3d361d8625cea3ec94
---

Acquisition is the yt-dlp-backed context (`services/go-api/internal/acquisition/`) that finds, ranks, downloads, verifies, tags, and stores audio for a saved Track. It is a customer of catalog, not an aggregate owner: it consumes `catalog/domain.Track` and its `MarkReady`/`MarkFailed`/`RevertToPending` invariants, and there is deliberately no `domain/` folder here — this context is orchestration-heavy, not aggregate-heavy.

## Subsystems

- [pipeline](pipeline.md) — the six-step Step chain (search → select → download → tag → store → update_track), its ports/adapters (yt-dlp, ffprobe, id3), and the `track_acquisition_*` events it publishes
- [scheduling](scheduling.md) — `BackgroundAcquisitionScheduler`'s semaphore-gated, dedup'd, panic-recovering job execution, and the `jobLog` operator-console telemetry it feeds
- [retry](retry.md) — `RetryAdmission`'s whole-policy gate for manual re-acquisition (failed-state-only, per-track cooldown)

Scheduling drives the pipeline via `AcquireTrackAudioService.Execute` and threads a `jobReporter` through its context so pipeline steps can report live progress without importing the scheduler; retry admission is a separate policy gate in front of the same `Schedule` entry point scheduling exposes.
