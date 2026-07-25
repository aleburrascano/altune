# One-off maintenance commands — router

Operator-run repair commands for the `api` binary: `api <command> [--execute] [--limit N]`.

Layout:

- `common.go` — shared CLI helpers (audio store construction, duration probing).
- `reacquire.go` — the shared re-acquisition loop (`runReacquire`), `reacquireSpec`, `reacquireTrack`, `expectedDuration`.
- `backfill_m4a.go` — re-acquires ready tracks stored as `.mp3`.
- `reacquire_corrupt.go` — re-acquires ready tracks stored as `.m4a`.
- `reconcile_truncated.go` — repairs tracks with a NULL duration.
- `backfill_duration.go`, `dedup_migration.go`, `fix_audio_refs.go`, `health_check.go` — independent one-shot repairs.

## Rules

- Every command is dry-run by default; only `--execute` may write. `--limit N` (`<= 0` means all) caps rows touched.
- Every command must stay safe to re-run.
- Order per track is store the new file → swap `audio_ref` → delete the old one, never the reverse.
- Never delete the old file when the DB update failed.
- Never leave a failed or gate-rejected re-acquire having mutated the file or the row.
- Always pass `reacquireTrack` an expected duration, or the prober cannot reject a wrong-length recording.

Why each rule exists, and what the 45s truncation threshold is for: `okf/backend/acquisition/pipeline.md`.
