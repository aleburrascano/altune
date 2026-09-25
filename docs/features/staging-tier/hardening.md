# Staging tier — isolation hardening (task #1526)

Staging runs on the **same VM** as prod as a separate Compose project (design Decision 1),
and for the walking-skeleton bring-up (#1490) its `.env.staging` was seeded from prod's
`.env.production`. That copy means staging currently shares some prod credentials and host
state, so a staging run could reach into prod's object storage, alert channels, or shared
files. This page records the per-resource sharing decision and what is fixed here vs. what
the operator must still provision.

Two audiences:

- **done-here** — enforced by committed config in this repo (`compose.staging.yml`), so it
  holds on the next deploy with no operator action.
- **operator-follow-up** — a value that lives only in `services/go-api/.env.staging` on the
  VM (orchestrator-managed, never in the repo). Code cannot fix these; the operator must
  swap in a staging-scoped credential. Each is called out below.

## Shared host mounts — done-here

`compose.staging.yml` mounts two host files that are shared with prod (tier-independent
acquisition-tool config). Both are now mounted **read-only (`:ro`)** for the staging
containers, so a staging run cannot mutate this prod-shared state:

| Mount | Container path | Why read-only is safe |
|---|---|---|
| `/home/ubuntu/altune/cookies.txt` | `/data/cookies.txt` | yt-dlp is invoked with `--cookies`; its end-of-run write-back of refreshed cookies degrades to a warning on a read-only file and does **not** abort the download. That write-back is precisely how a staging run could otherwise clobber prod's live cookie jar, so blocking it is the isolation win, not a regression. |
| `/home/ubuntu/altune/streamrip.toml` | `/home/altune/.config/streamrip/config.toml` | streamrip only reads this config at runtime. `Fetch` invokes the binary with `--no-db` (`internal/acquisition/adapters/streamrip/source.go:108`), so streamrip never opens or creates a download database beside the config. Pure read. |

Verified: `docker compose -f services/go-api/deploy/compose.staging.yml config` resolves with
`read_only: true` on both mounts for `go-api-blue` and `go-api-green`.

## Per-key decisions for `.env.staging` (VM)

The committed template (`services/go-api/deploy/.env.staging.example`) is already staging-scoped
for the auth/DB/redis/overseer realm. The rows below cover the keys that were inherited from
prod and are the isolation risks flagged in #1490/#1512. Keys not listed are already isolated
by the template or carry no prod-mutating capability.

### Must be staging-scoped (a staging write could touch prod)

| Key(s) | Risk | Decision | Status |
|---|---|---|---|
| `OCI_S3_BUCKET` / `OCI_S3_ACCESS_KEY` / `OCI_S3_SECRET_KEY` / `OCI_S3_ENDPOINT` | S3 object storage is **write-capable**. Sharing prod's bucket + creds means a staging upload/delete lands in prod object storage. | Give staging a **distinct bucket** (or a distinct key prefix on a bucket staging owns), **or** read-only OCI creds scoped away from the prod bucket. Do not reuse prod's write creds against prod's bucket. | **operator-follow-up** — provision a staging bucket/prefix or scoped creds and set these four keys in the VM `.env.staging`. Overseer's own OCI path is already off (`OVERSEER_OCI_ENABLED=false`). |
| `GITHUB_ISSUE_TOKEN` / `GITHUB_ISSUE_REPO` | In-app feedback creates **real GitHub issues** (write-capable PAT). Sharing prod's token means a staging feedback submit files a public issue on the prod repo, tagged with a real reporter UUID. | Point staging at a **throwaway/scratch repo** with its own PAT, **or** disable feedback (`FEEDBACK_ENABLED=false`) so staging never writes issues. | **operator-follow-up** — set `FEEDBACK_ENABLED=false` (simplest) or scope a staging repo+token in the VM `.env.staging`. |
| `BEHAVIORAL_CORPUS_PATH` | If set to a host path shared with prod, the nightly corpus job **writes** there and staging labels contaminate prod's corpus. | Leave **empty** on staging (job off), or point at a staging-only path. | **operator-follow-up** — confirm empty/staging-only in the VM `.env.staging`. |

### Safe to share (read-only external quota only)

| Key(s) | Rationale | Status |
|---|---|---|
| `LASTFM_API_KEY` / `LASTFM_SHARED_SECRET` / `FANARTTV_API_KEY` / `GENIUS_ACCESS_TOKEN` / `DISCOGS_TOKEN` / `YOUTUBE_API_KEY` / `ACOUSTID_API_KEY` | Read-only discovery/metadata providers — no prod state is mutated by sharing. The only shared resource is **API quota**; `EVAL_METER_ENABLED=false` (forced in `compose.staging.yml`) already keeps staging smoke runs from burning prod's provider budget. | **done-here** (quota guard) — keep watching quota; move a key to staging-scoped only if a provider's rate limit starts hurting prod. |

### Staging contact hygiene

| Key | Rationale | Status |
|---|---|---|
| `MUSICBRAINZ_USER_AGENT` | MusicBrainz requires a real contact URL/email in the user agent; the template ships the placeholder `mailto:dev@altune.test`. Not a prod-mutation risk, but MusicBrainz can rate-limit or block a bogus contact. | **operator-follow-up** — set a real reachable contact for staging in the VM `.env.staging` (a staging-specific `mailto:` is fine and keeps staging traffic attributable separately from prod). |

## Summary — what still needs the operator

Committed config closes the shared-mount write path. The remaining isolation gaps live only
in the VM `.env.staging` and need the operator to provision staging-scoped values:

1. **OCI_S3** — a distinct staging bucket/prefix or read-only creds (highest priority: this
   is the only path that can silently write into prod object storage).
2. **GITHUB_ISSUE_TOKEN/REPO** — `FEEDBACK_ENABLED=false` or a scratch repo+token.
3. **BEHAVIORAL_CORPUS_PATH** — empty or staging-only.
4. **MUSICBRAINZ_USER_AGENT** — a real staging contact.
