# shared/acquisition — router

Client-side tracking of in-flight Track downloads: `downloadStore.ts` (`useDownloadStore`, membership + forward-only phase + terminal-dwell timers), `trackStatusStore.ts` (`useTrackStatusStore`, per-track acquisition status plus the identity→trackId index for unowned/preview tracks), `stagePhase.ts` (backend pipeline stage → display phase/label), `audioCacheInvalidation.ts` (registration seam for `features/playback`'s on-disk audio cache), `useActiveDownloads.ts` (thin re-export of the dock's active-items selector), and `ui/DownloadsBar.tsx` / `ui/DownloadsSheet.tsx`.

Invariants:

- The store is fed exclusively by `shared/events/applyServerEvent.ts` through its imperative API (`startDownload`/`progressDownload`/`completeDownload`/`failDownload`, `patchTrackStatus`/`removeTrackStatus`, `linkTrackIdentity`/`unlinkTrackIdentity`) — never mutate `entries`/`statuses`/`identities` directly outside this directory.
- `PHASE_RANK` is forward-only: a progress event ranked below the current phase, or any event after a terminal `done`/`failed`, is rejected. A re-acquire re-enters via `start()`.
- `complete()`/`fail()`/`start()`/`reset()` each cancel the track's (or, for `reset()`, every track's) pending scheduled timers before scheduling their own — a stale terminal-dwell callback must never fire against state a later call already moved past.
- `STAGE_TO_PHASE`'s keys must equal the acquisition pipeline's step names in `services/go-api/internal/acquisition/service/step_*.go`, derived from the Go source at test time.
- A component that renders server-mutable state from these stores subscribes directly to it (`DownloadsSheet`'s `DownloadRow` via `useDownloadPhase`) rather than reading a prop snapshot.

Tests: `__tests__/` — `downloadStore`, `trackStatusStore`, `stagePhase`, `useActiveDownloads`, `audioCacheInvalidation`; `ui/__tests__/` — `DownloadsBar`, `DownloadsSheet`. Categories and rejections: `okf/testing/shared-acquisition.md`.

Knowledge base: `okf/mobile/shared-acquisition.md` — read before structural work; update in the same commit when behavior it describes changes (pre-commit hook enforces).
