---
name: acquisition-debug
description: Debug audio acquisition (a track that won't download, stays pending, fails with "Couldn't find this track" / "Download failed", or a nameless "Downloading track" bar) against the live backend before reading any code. Pulls real outcomes from the prod database, probes every audio source from the prod IP with the app's own yt-dlp flags, and reads the running go-api's logs, all read-only. Trigger words: download didn't work, track stuck pending, acquisition failing, no match found, is YouTube blocked, why did this track fail.
---

# Acquisition debug

Evidence first, code second. An acquisition complaint is almost always one of three things: a **source stopped working** from the prod IP (stale YouTube cookies, a yt-dlp that YouTube outgrew, SoundCloud handing back 30s previews), the **matcher rejected** every candidate, or the **job never settled**. The live data tells you which one in a minute, and reading the pipeline first skips that step. `scripts/acq-debug.sh` runs everything over SSH on the OCI VM named by `ALTUNE_HOST` (user@host or an ssh config alias). It is read-only: the DB session is forced read-only, and yt-dlp runs `--simulate` on a scratch copy of the cookie jar.

## 1. Is every source alive right now?

```bash
bash scripts/acq-debug.sh tools
```

Each canary shows `OK`, `PREVIEW ONLY` or `FAIL <yt-dlp error>`. A YouTube `FAIL` saying "Sign in to confirm you're not a bot" or "The page needs to be reloaded" means the cookie jar (`~/altune/cookies/cookies.txt` on the VM, which go-api renews after each successful yt-dlp run) has gone stale, or the pinned yt-dlp (`services/go-api/deploy/Dockerfile`) is too old. Both need the user: cookies come from their browser, and a bump ships through the deploy pipeline.

How long a jar lasted is on record in `~/altune/cookies/cookies.txt.session.json`. Prod and staging share it, so it survives whichever one met the bot check first, and a recreated container. `first_signin_failure` holds `at`, `session_age_hours` (since the jar was dropped in), `run`, `write_backs` and `environment`. It is written once per dropped-in jar. `signed_out_since` is set by any bot check and cleared by the next YouTube run that succeeds on the same jar, and while it is set the `youtube_cookies_signed_out` alert emails the drop-in command. A new export resets both. The first-failure fields (plus `installed_at`) are logged at `acquisition.cookie_session_first_signin_failure` by whichever environment hit it. Reading the session file is fine: it holds a hash of the jar, never cookies. Never print `cookies.txt` itself.

To drop in a new jar, copy the browser export anywhere on the VM, then from `~/altune/services/go-api` run `bash deploy/cookie-jar-dir.sh <export>`, and delete the copied export once it succeeds. It refuses an export that is empty, lacks the `# Netscape HTTP Cookie File` header, or has no youtube.com `LOGIN_INFO` cookie (signed out), and touches nothing when it refuses. Otherwise it copies the export to `~/altune/cookies/cookies.txt.new` at 0660 in go-api's group, and, as go-api's user under go-api's own `cookies.txt.lock`, keeps the live jar as `cookies.txt.prev` and renames the new one over `cookies.txt`. The rename is atomic and a renewal can't land on top of it, and the new contents start a new session on record. To undo the last drop-in, run `bash deploy/cookie-jar-dir.sh --rollback`: it swaps `cookies.txt.prev` and `cookies.txt` the same way, so a second rollback redoes it. Never copy a jar in by hand: under a 0077 umask, or from a 0600 export, the copy lands without group read, go-api (uid 1000, which reads it through group 1000) cannot open it, and every yt-dlp run fails with `copy cookie jar: permission denied` until the script is rerun. Never write to the old `~/altune/cookies.txt`: go-api doesn't read it, and rsync or an editor would replace any symlink there with a stale copy that is silently ignored. If a jar is already sitting there, install it with `bash deploy/cookie-jar-dir.sh ~/altune/cookies.txt`, which removes it afterwards.

A renewal yt-dlp hands back without a session cookie the live jar had is not kept, and logs INFO `acquisition.cookie_renewal_refused` with `run` and `missing_cookies`.

Done when: you know which sources can deliver today.

## 2. What actually happened?

```bash
bash scripts/acq-debug.sh summary [days]      # status mix, failure codes, which source delivered, stuck pending
bash scripts/acq-debug.sh track "<title|uuid>" # the row + that track's log lines
```

Read `failure_reason` literally. `all N candidates rejected (7 download, 3 identity, …)` counts each judged candidate once (by source key, across both tiers) and why it died: `download` points to the source (step 1), `identity`/`duration` point to the matcher (`internal/download/service/matching.go`), and a trailing `, M not tried` counts candidates never judged (attempt budget ran out or the source was dead), which are not rejections. The per-day "which source delivered" table shows when a source went dark: a source that vanishes from it is the outage start date.

Done when: you can name the failing stage for the user's track.

## 3. Reproduce the search from prod

```bash
bash scripts/acq-debug.sh probe "<artist> <title>"
```

This runs the app's own searches (`ytsearch5`, `scsearch5`) and then tries extracting each candidate the way the download step would, printing duration and uploader or the exact error. 30.0s from SoundCloud is a Go+ preview. If extraction works here but the track failed, the problem is in the matcher, not the source.

Done when: you have seen the candidate list the pipeline saw.

## 4. Only then, the code

`logs [since] [regex]` gives the running container's acquisition lines. They only cover that container's lifetime, because every deploy starts a fresh container, and the DB is the only durable history. `sql "<select>"` answers anything else, read-only.

A real failure worth keeping becomes a permanent eval case with:

```bash
bash scripts/acq-debug.sh [--staging] capture "<title|uuid>"
```

It pulls the track row plus its `candidate_evaluated` and `acquisition.rejection_summary` log lines and turns them into one `eval.Case` JSON object (`class: "RW"`), printed to stdout. It never commits or edits goldens: review the output, then add it under `services/go-api/internal/download/internal/adapters/qualitycheck/goldens/` yourself.

To find tracks that were stored with an unrequested version (Instrumental, reaction, and other veto qualifiers):

```bash
bash scripts/acq-debug.sh [--staging] audit-qualifiers [limit]
```

It lists ready tracks, fetches each stored source's title from the public oEmbed endpoints (about 2 requests per second, default limit 500), and prints `track_id`, `title`, `artist`, `source_title`, `veto_words` for each flagged track, then `flagged N of M`. It only reads; re-acquiring what it finds is the operator's call, batch by batch.

To score whether each stored YouTube track's audio came from the right upload (music video, user upload, fan channel, clean edit, video or edit qualifiers, duration drift):

```bash
bash scripts/acq-debug.sh [--staging] export-tracks [days] > tracks.jsonl
cd services/go-api && go run ./cmd/acquisitioneval live -tracks ../../tracks.jsonl -out judged.jsonl
```

`export-tracks` is read-only. `live` queries YouTube Music and oEmbed from the machine it runs on (paced by the shared InnerTube budget; `ACQEVAL_LIVE_TIMEOUT` bounds each call), writes one judged row per track and prints a count per flag, then `flagged N of M`. A lookup that fails shows as `lookup_failed:<step>`; SoundCloud and other hosts carry a `skipped:<host>` marker.

Anything that writes to prod (retrying a track, failing a stuck row, replacing cookies) needs the user's yes first. Name the exact change and wait for approval.
