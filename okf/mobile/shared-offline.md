---
type: Subsystem
title: Offline downloads (pinned tracks)
description: User-pinned local copies of library audio that survive app kills and play with no network, kept in the document directory and reconciled against disk at startup.
resource: apps/mobile/src/shared/offline/
tags: [mobile, offline, storage, playback]
verified_commit: 98dcd6a8b6c41a7ceeab71d24771c1752819a8eb
---

Pinning a track downloads the real audio object to this device and makes playback prefer it over any network URL. It is the "plays on a plane" promise, and every design choice here follows from that promise being unconditional.

## Not the same thing as the prefetch cache

`features/playback/audioPrefetch.ts` looks similar and is a different concern:

| | prefetch | pinned |
|---|---|---|
| root | `Paths.cache` | `Paths.document` |
| lifetime | evicted on a 4-track window; the OS may purge it under storage pressure | deleted when the user unpins, or when the signed-in account changes (see Identity boundary) |
| meaning | an optimisation — avoids a buffer stall on auto-advance | a promise — this track plays offline |

Putting pinned audio in the cache directory would let iOS reclaim it silently, which is exactly the failure the feature exists to prevent. The two never share a root or an eviction policy.

## Files win over the index

`pinnedStore` persists an index (`Paths.document/offline/pinned.json`) so a relaunch knows what it holds without re-listing and parsing the directory on every read. But the **files are the source of truth**. `reconcile()` runs once per launch (via `OfflineReconcileBridge`, mounted inside `PlaybackProvider`) and rebuilds the index from what is actually on disk:

- entry with a file → `ready`, uri refreshed from the file
- entry with no file, previously `queued`/`downloading` → re-queued (a kill mid-download must retry, not be forgotten)
- entry with no file, previously `ready` → **dropped**

That last case is the one that matters. An index claiming a track is downloaded when the file is gone (restore-from-backup, a manual clean) produces a track that shows as available offline and then fails to play with no network — the worst possible outcome for this feature, and worse than showing nothing.

Which is exactly why **an unreadable directory is not an empty one** (2026-07-30). `pinnedFilesOnDisk` swallows a listing failure and returns `[]`, so for a while `findPinned` answered "no file" identically whether the directory was empty or unlistable. A transient read error at launch — an iOS data-protection window during a background wake is the realistic one — therefore made `reconcile` conclude that *every* file had vanished, drop the whole pinned library, and persist that emptied index. The audio stayed on the device as orphaned bytes no code path could ever find again, since there is no directory sweep. `reconcile` now returns early on `pinnedDirReadable() === false`, leaving the index exactly as it was for the next launch to reconcile. Skipping a pass is always recoverable; persisting a false mass-unpin is not.

The honest limit of the "a file means ready" rule: `findPinned` matches `<trackId>.*` and has no completeness check, so a truncated file left by a killed download is indistinguishable from a whole one and is adopted as `ready`. That reaches the same "available offline, fails to play" state through a different door. Closing it needs a completion marker — a `.part` suffix rewritten on success, or a recorded expected size — which is deliberately not built, and is why this section does not claim a hard invariant either.

## Sequential downloads

The worker downloads one track at a time, guarded by `isWorking` for re-entrancy. Three concurrent downloads of a 10 MB track on a phone hotspot produces three timeouts rather than one file. `pinMany` (playlist "Download all") therefore enqueues rather than fanning out.

Both entry points share one admission rule, `needsDownload`: **only an absent or `failed` entry is enqueued.** `ready` and `downloading` were always excluded; `queued` was not, so a second "Download all" over a batch still working through its queue re-admitted every id that had not yet reached the front, pushing duplicates that the worker then downloaded twice. Retry is the one case that must pass, and `failed` is the only status that means it.

Downloads resolve through the same `fetchAudioUrls` presigned-URL path playback uses — a pinned copy is a real download of the real object, not a second representation of it. A download that is superseded while in flight is discarded rather than published: the post-await check asks whether the entry is **still `downloading`**, not merely whether it still exists. Existence was too weak a test — see Re-pinning below, where the two answers diverge.

## The playback seam

`loadNativeTrack.signedUrl` is the single place a library track's URL is chosen, in preference order: **pinned local file → presigned URL → authenticated proxy**. The pinned check goes first and is the entire point: once a track is on disk, playback must not depend on a signed URL that expires or a network that is not there.

## Surfaces

Track context menu (`library/ui/trackMenu.ts`, the one assembly point) gains Download / Cancel / Remove / Retry by status, gated on `acquisition_status === 'ready'` — offline only means something once the server actually holds the audio. `LibraryRow` shows a check when pinned and a down-arrow while downloading, distinct from the acquisition phase indicator (that one is the *server* fetching audio; this is a copy on *this device*). Playlist detail offers "Download all (N)" / "Download rest (N)" / "Remove downloads". Settings shows a track count and a byte size plus remove-all. Note the two come from different places: the size is read from disk (`pinnedBytes()`, so it reports space actually used) while the count is summed from the index (`SettingsScreen.tsx` filters `pinnedEntries` for `ready`). They can therefore disagree — an earlier version of this document claimed both came from disk.

## Identity boundary (2026-07-24)

Pinned downloads are **not user-scoped** — one index at `Paths.document/offline/pinned.json`, one audio directory, loaded once at module init. Nothing in the file layout distinguishes whose tracks these are, so the boundary has to be enforced at the moment identity changes rather than by partitioning storage.

`useSession` therefore calls `unpinAll()` when the signed-in `user.id` *changes* — not on every auth event, and never on the first observation (its `seededRef` guard), so a cold start into an existing session keeps your downloads. This sits alongside the query-cache clear that hook already owned (see [shared-auth](shared-auth.md)).

Before this, signing out left the previous account's audio in the document directory indefinitely. Being precise about the harm: the next user could **not** play those tracks. Both the index lookup (`pinnedUri`) and the filename match (`findPinned`) are keyed by track UUID, and a different account's library never names one, so the audio was unreachable. What actually leaked was disk residue plus a misreported UI — the surviving index fed Settings both a byte size and a *track count* belonging to the previous user. Only an explicit "Remove downloads" tap or an uninstall reclaimed it.

The sequential worker had a matching hole: `downloadPinned` writes the file, then `mark` drops the index write if the entry vanished mid-flight, and `reconcile()` only ever walks entries that exist — so a file downloaded across a sign-out became an orphan no startup pass could see. `downloadOne` now re-reads the entry once the download settles and deletes the file when that entry is no longer `downloading`, on **both** the success and the failure path (a rejection after `File.downloadFileAsync` has written bytes strands a partial file just as readily).

The honest limit of that guarantee: deletion is best-effort, so a file can still outlive its index entry. `deleteAllPinned` deletes per file rather than aborting the sweep on the first throw, but a file the OS refuses to delete stays while `unpinAll` clears the index regardless; and if `pinnedDir().list()` throws, `findPinned` returns null and `reconcile()` drops otherwise-`ready` entries whose files are still on disk (documented in the reconcile rules above). Both leave invisible residue. Closing it properly needs a directory sweep that can delete files with no index entry — deliberately not built, and the reason this section does not claim a hard invariant.

The prefetch cache (`features/playback/audioPrefetch.ts`) is deliberately *not* cleared on sign-out. It lives in `Paths.cache` (OS-purgeable) and is keyed by track UUID so no other account can resolve it. Left alone as inert.

## Re-pinning after the audio underneath changes (2026-07-26)

A pinned file is keyed by track id, and a re-acquire replaces the audio at the *same* object key — so the ref the client holds stays valid while the bytes behind it change, and `signedUrl` prefers `pinnedUri` over the freshly presigned URL unconditionally. A downloaded track therefore kept playing the old recording forever after a successful re-acquire, which is indistinguishable from "re-acquire does nothing".

`repinIfPinned` closes it: on `track_acquisition_completed` the entry is unpinned (deleting the file) and re-pinned (queueing a fresh download), but only when an entry already exists — completing an acquisition must never *start* pinning a track the user never downloaded. It is deliberately a delete-then-download rather than an in-place overwrite, so a failed re-download leaves a `queued`/`failed` entry the existing `reconcile` pass already understands rather than a stale file wearing a fresh label.

That unpin-then-pin pair is also why the worker's cancellation check had to stop asking "does the entry still exist" (2026-07-30). Both halves run **synchronously**, so when the event arrives for a track that is *currently downloading*, the entry is gone and back again before the in-flight download settles — and the check found it present and published the superseded bytes as `ready`, briefly serving the old recording to `pinnedUri` under a status that claims the re-acquire finished. Only the queued re-pin sitting behind it corrected the state. Asking whether the entry is still `downloading` answers the question that was always meant: every way a status leaves `downloading` mid-flight — unpin, `unpinAll`, re-pin, a reconcile requeue — means this particular download has been superseded, and existence detected exactly one of the four.

The prefetch cache is invalidated through the same event by a different route: `shared/acquisition/audioCacheInvalidation` is a registration seam (`registerAudioCacheInvalidator`), because `applyServerEvent` lives in `shared/` and the cache lives in `features/playback/`. `playbackService()` registers `evictCached` at startup; nothing is wired when playback never starts, which is correct — there is no cache to clear.
