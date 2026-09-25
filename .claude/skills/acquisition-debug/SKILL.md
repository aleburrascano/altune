---
name: acquisition-debug
description: Debug audio acquisition (a track that won't download, stays pending, fails with "Couldn't find this track" / "Download failed", or a nameless "Downloading track" bar) against the live backend before reading any code. Pulls real outcomes from the prod database, probes every audio source from the prod IP with the app's own yt-dlp flags, and reads the running go-api's logs, all read-only. Trigger words: download didn't work, track stuck pending, acquisition failing, no match found, is YouTube blocked, why did this track fail.
---

# Acquisition debug

Evidence first, code second. An acquisition complaint is almost always one of three things: a **source stopped working** from the prod IP (stale YouTube cookies, a yt-dlp that YouTube outgrew, SoundCloud handing back 30s previews), the **matcher rejected** every candidate, or the **job never settled**. The live data tells you which one in a minute, and reading the pipeline first skips that step. `scripts/acq-debug.sh` runs everything over SSH on the OCI VM. It is read-only: the DB session is forced read-only, and yt-dlp runs `--simulate` on a scratch copy of the cookie jar.

## 1. Is every source alive right now?

```bash
bash scripts/acq-debug.sh tools
```

Each canary shows `OK`, `PREVIEW ONLY` or `FAIL <yt-dlp error>`. A YouTube `FAIL` saying "Sign in to confirm you're not a bot" or "The page needs to be reloaded" means the cookie jar (`/home/ubuntu/altune/cookies.txt` on the VM) has gone stale, or the pinned yt-dlp (`services/go-api/deploy/Dockerfile`) is too old. Both need the user: cookies come from their browser, and a bump ships through the deploy pipeline. `streamrip services: none` means the streamrip source is off.

Done when: you know which sources can deliver today.

## 2. What actually happened?

```bash
bash scripts/acq-debug.sh summary [days]      # status mix, failure codes, which source delivered, stuck pending
bash scripts/acq-debug.sh track "<title|uuid>" # the row + that track's log lines
```

Read `failure_reason` literally. `all N candidates rejected (7 download, 3 identity, …)` counts why each candidate died: `download` points to the source (step 1), `identity`/`duration` point to the matcher (`internal/acquisition/service/matching.go`), and `not_attempted` means the attempt budget ran out first. The per-day "which source delivered" table shows when a source went dark: a source that vanishes from it is the outage start date.

Done when: you can name the failing stage for the user's track.

## 3. Reproduce the search from prod

```bash
bash scripts/acq-debug.sh probe "<artist> <title>"
```

This runs the app's own searches (`ytsearch5`, `scsearch5`) and then tries extracting each candidate the way the download step would, printing duration and uploader or the exact error. 30.0s from SoundCloud is a Go+ preview. If extraction works here but the track failed, the problem is in the matcher, not the source.

Done when: you have seen the candidate list the pipeline saw.

## 4. Only then, the code

`logs [since] [regex]` gives the running container's acquisition lines. They only cover that container's lifetime, because every deploy starts a fresh container, and the DB is the only durable history. `sql "<select>"` answers anything else, read-only. The module map lives at `services/go-api/internal/acquisition/ARCHITECTURE.md`.

Anything that writes to prod (retrying a track, failing a stuck row, replacing cookies) needs the user's yes first. Name the exact change and wait for approval.
