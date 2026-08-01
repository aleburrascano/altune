# shared/offline — router

User-pinned local copies of library audio: the on-disk files, the index that tracks them, and the launch reconciliation between the two.

Layout:

- `pinnedFiles.ts` — the `offline-audio/` directory and everything that touches it: `pinnedDir`, `pinnedDirReadable`, `findPinned`, `deletePinned`, `deleteAllPinned`, `pinnedBytes`, `downloadPinned`, plus `extFromUrl` and `formatBytes`.
- `pinnedStore.ts` — `usePinnedStore` (`pin`, `pinMany`, `unpin`, `unpinAll`, `reconcile`), the `pinned.json` index seam, the sequential download worker, and the exported `pinnedUri` / `repinIfPinned`.
- `OfflineReconcileBridge.tsx` — mounts `reconcile()` once per launch; renders nothing.
- `__tests__/` — `pinnedFiles.test.ts`, `pinnedFiles.property.test.ts`, `invariants.test.ts`, `pinnedStore.actions.test.ts`, `pinnedStore.index.test.ts`, `pinnedStore.persistence.test.ts`, `pinnedStore.pinnedUri.test.ts`, `pinnedStore.property.test.ts`, `pinnedStore.worker.test.ts`, `pinnedStore.repin.test.ts`, `pinnedStore.version.test.ts`, `pinnedStore.reconcile.test.ts`, `OfflineReconcileBridge.test.tsx`.

Dependencies: `@shared/api-client/audio` (`fetchAudioUrls`), `expo-file-system`, `zustand`. Consumed by `features/library`, `features/settings`, `features/playback/loadNativeTrack`, `shared/auth/useSession` and `shared/events/applyServerEvent`.

## Rules

- Pin audio under `Paths.document`, never `Paths.cache`.
- Rebuild the index from disk in `reconcile()` — the files are the source of truth, the index is a cache of them.
- Skip the reconcile pass when the audio directory cannot be listed; never read an unreadable directory as an empty one.
- Drop a `ready` entry whose file is gone rather than keeping it.
- Re-queue an entry that was `queued` or `downloading` and has no file.
- Download one track at a time behind `runQueue`'s `isWorking` guard; never fan out.
- Enqueue only an absent or `failed` entry — `queued`, `downloading` and `ready` are already in hand.
- Discard a download whose entry stopped being `downloading` while it was in flight.
- Re-pin only a track that already has an entry; completing an acquisition never starts a new pin.
- Keep `repinIfPinned` a delete-then-download, never an in-place overwrite.
- Persist only local `file://` uris — a signed URL never reaches the index.
- Return a uri from `pinnedUri` only for a `ready` entry.
- Record the `audio_version` a download was fetched under, and carry it through `reconcile()`.
- Refuse a pinned copy whose recorded version disagrees with the server's, and re-pin as you refuse.
- Treat an absent or empty expected version as "no expectation" and serve the local copy — never as a mismatch.
- Clear every pinned track when the signed-in user id changes.

Why each rule exists, and the limits this design deliberately accepts: `okf/mobile/shared-offline.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces). Test-category verdicts: `okf/testing/shared-offline.md`.
