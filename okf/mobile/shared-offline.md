---
type: Subsystem
title: Offline downloads (pinned tracks)
description: User-pinned local copies of library audio that survive app kills and play with no network, kept in the document directory and reconciled against disk at startup.
resource: apps/mobile/src/shared/offline/
tags: [mobile, offline, storage, playback]
verified_commit: 650555c091fab169d723fa9bd938c0ab97f89541
---

Pinning a track downloads the real audio object to this device and makes playback prefer it over any network URL. It is the "plays on a plane" promise, and every design choice here follows from that promise being unconditional.

## Not the same thing as the prefetch cache

`features/playback/audioPrefetch.ts` looks similar and is a different concern:

| | prefetch | pinned |
|---|---|---|
| root | `Paths.cache` | `Paths.document` |
| lifetime | evicted on a 4-track window; the OS may purge it under storage pressure | deleted only when the user unpins |
| meaning | an optimisation — avoids a buffer stall on auto-advance | a promise — this track plays offline |

Putting pinned audio in the cache directory would let iOS reclaim it silently, which is exactly the failure the feature exists to prevent. The two never share a root or an eviction policy.

## Files win over the index

`pinnedStore` persists an index (`Paths.document/offline/pinned.json`) so a relaunch knows what it holds without re-listing and parsing the directory on every read. But the **files are the source of truth**. `reconcile()` runs once per launch (via `OfflineReconcileBridge`, mounted inside `PlaybackProvider`) and rebuilds the index from what is actually on disk:

- entry with a file → `ready`, uri refreshed from the file
- entry with no file, previously `queued`/`downloading` → re-queued (a kill mid-download must retry, not be forgotten)
- entry with no file, previously `ready` → **dropped**

That last case is the one that matters. An index claiming a track is downloaded when the file is gone (restore-from-backup, a manual clean) produces a track that shows as available offline and then fails to play with no network — the worst possible outcome for this feature, and worse than showing nothing.

## Sequential downloads

The worker downloads one track at a time, guarded by `isWorking` for re-entrancy. Three concurrent downloads of a 10 MB track on a phone hotspot produces three timeouts rather than one file. `pinMany` (playlist "Download all") therefore enqueues rather than fanning out.

Downloads resolve through the same `fetchAudioUrls` presigned-URL path playback uses — a pinned copy is a real download of the real object, not a second representation of it. A track unpinned mid-download is not resurrected when its download completes (`mark` checks the entry still exists).

## The playback seam

`loadNativeTrack.signedUrl` is the single place a library track's URL is chosen, in preference order: **pinned local file → presigned URL → authenticated proxy**. The pinned check goes first and is the entire point: once a track is on disk, playback must not depend on a signed URL that expires or a network that is not there.

## Surfaces

Track context menu (`library/ui/trackMenu.ts`, the one assembly point) gains Download / Cancel / Remove / Retry by status, gated on `acquisition_status === 'ready'` — offline only means something once the server actually holds the audio. `LibraryRow` shows a check when pinned and a down-arrow while downloading, distinct from the acquisition phase indicator (that one is the *server* fetching audio; this is a copy on *this device*). Playlist detail offers "Download all (N)" / "Download rest (N)" / "Remove downloads". Settings shows track count and bytes read from disk (not summed from the index — the number people check is space actually used) plus remove-all.
