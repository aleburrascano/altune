---
type: Subsystem
title: Shared api-client
description: Typed HTTP client wrapping the go-api backend — auth header injection, per-context typed functions, and hand-maintained wire types.
resource: apps/mobile/src/shared/api-client/
tags: [mobile, shared, api-client, http, auth]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

`index.ts` is the base layer: `apiBase` (from `EXPO_PUBLIC_API_URL`, defaulting to `http://127.0.0.1:8000`), the `ApiError` class (`status` + `message`), and `apiFetch<T>(path, init?)` — the single fetch wrapper every typed client function calls. Per ADR-0006, `apiFetch` unconditionally injects `Authorization: Bearer <access_token>` from the current Supabase session (`supabase.auth.getSession()`) when one exists (see [[auth-feature]]); a stale/missing session is swallowed (caught, not rethrown) and the request proceeds unauthenticated, letting the backend 401 and the app's `AuthGate` handle redirect-to-sign-in. It also sets `ngrok-skip-browser-warning` (an AIDEV-NOTE flags this as removable once off ngrok). Non-2xx responses throw `ApiError`; `204`/`304` short-circuit to `undefined as T`.

Per-context files each export typed functions calling `apiFetch` with a specific path/verb, plus the response/request shapes (hand-maintained, not codegen'd — `types.ts`'s header explicitly flags this as a sync risk mitigated by the plan-reviewer's grep, pending a future OpenAPI codegen spec). `tracks.ts`: `getTracks`, `createTrack`, `deleteTrack`, `retryAcquisition` against `/v1/tracks`. `playlists.ts`: full CRUD + membership (`addTrackToPlaylist`, `reorderPlaylistTracks`) against `/v1/playlists`. `playback.ts`: `getQueueState`/`saveQueueState` against `/v1/playback/queue-state` — the server-side resume-on-reopen snapshot (the memento's caretaker, see [[playback]]) backing the Queue subsystem (see [[shared-playback]]). `discovery.ts` is by far the largest: `searchDiscovery` (normalizes wire `subtitle`/`image_url` `undefined`→`null` via `normalizeResult` since Go's `omitempty` drops empty fields), `suggestDiscovery`, `listSearchHistory`, `recordEvent` (stamps `session_id` from `@shared/telemetry/session` onto every event's JSONB payload), catalog-browse functions (`getAlbumTracks`, `getArtistTopTracks`, `getArtistAlbums`, `getRelatedTracks`), and one function per detail-enrichment provider (`getEnrichment` for MusicBrainz, `getDiscogsEnrichment`/`getDiscogsArtistEnrichment`, `getLastFmEnrichment`, `getDeezerEnrichment`, `getLyrics`) — each with its own response type documenting "collections always present, unresolved entity returns empty payload" as the null-object contract.

`types.ts` holds the catalog wire types (`TrackResponse`, `AcquisitionStatus`, `PlaylistResponse`, etc.) consumed by `tracks.ts`/`playlists.ts` and by feature code needing the raw response shape.

Consumed by every feature that talks to the backend — discover, library, playlists, playback.
