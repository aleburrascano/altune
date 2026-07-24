# One-off maintenance commands — router

Operator-run repair commands for the `api` binary, invoked as `api <command> [--execute] [--limit N]`. Every command is **dry-run by default**; `--execute` applies, and `--limit N` (`<= 0` means all) caps the rows touched. All of them are safe to re-run.

Layout:

- `common.go` — shared CLI helpers (audio store construction, duration probing).
- `reacquire.go` — the shared re-acquisition loop (`runReacquire`) plus `reacquireSpec`, the per-command differences: which `audio_ref` values to select and how to label the output. `reacquireTrack` runs the gated acquisition pipeline (search → select → download → verify → tag → store) for one track and returns the new `audio_ref` without touching the database or the old file.
- `backfill_m4a.go` — re-acquires ready tracks stored as `.mp3`.
- `reacquire_corrupt.go` — re-acquires ready tracks stored as `.m4a`.
- `reconcile_truncated.go` — repairs tracks with a NULL duration.
- `backfill_duration.go`, `dedup_migration.go`, `fix_audio_refs.go`, `health_check.go` — independent one-shot repairs.

Invariants:

- Order per track is store the new file → swap `audio_ref` → delete the old one, never the reverse. If the DB update fails the new file is already stored but the row still points at the old ref, so the track keeps playing; a later run re-processes it, leaving at worst one orphaned file. **Never delete the old file on that path.**
- A failed or gate-rejected re-acquire leaves the existing file and row untouched — a broken original is preferable to a lost row.
- `reacquireTrack` is passed an expected duration (`expectedDuration`: the DB value, else probed from the existing file) so `DownloadStep`'s prober rejects a wrong-length recording.
- `truncatedAudioThresholdSecs` (45s) is the line between a truncated preview and a real track. SoundCloud previews are ~30s and the affected tracks all probe at 29.8s; no track this library cares about is under 45s. Above the threshold the audio is fine and only `duration_seconds` is backfilled — no re-download. Below it, the track is marked failed and `audio_ref` cleared so the app offers a retry that re-acquires via the search pipeline.

Historical context: `backfill_m4a` originally converted MP3 → native M4A. After the ID3-on-m4a corruption the pipeline was reverted to MP3, so both it and `reacquire_corrupt` now simply re-run tracks through the current pipeline, whose output extension follows whatever that pipeline produces.
