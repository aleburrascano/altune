import TrackPlayer, { type AddTrack } from 'react-native-track-player';

import { audioRequestHeaders, audioStreamUrl, fetchAudioUrls } from './api/audio';
import { ensurePlayerSetup } from './initPlayer';
import { beginNativeLoad, endNativeLoad } from './nativeSyncGuard';
import type { PlaybackTrack } from '@shared/playback/types';

export interface LoadNativeTrackOptions {
  // When false, the track is loaded but not started — used to resume a queue
  // paused at a saved position so the user presses play to continue.
  autoplay?: boolean;
  // Seek to this offset (ms) after loading. 0 starts from the top.
  startPositionMs?: number;
}

// Cap on presigned URLs minted per load: only the near-term window is signed so
// the resolve stays fast (it blocks queue start). Tracks past the window fall
// back to the proxy until reached; download-ahead covers the imminent next track
// regardless.
const MAX_PRESIGN = 25;

// AIDEV-NOTE: Last-write-wins token for native loads. A load is a multi-await
// sequence (setup → resolve URLs → reset → add → skip), so two overlapping loads
// would interleave their reset()/add() calls and leave the native queue a mix of
// both. Every load claims a token first; after each await it bails if a newer
// load has claimed one. The newest caller always wins, which is what the user
// expects: tapping a track must beat an in-flight resume, not race it.
let loadToken = 0;

function claimLoad(): number {
  return ++loadToken;
}

function isStale(token: number): boolean {
  return token !== loadToken;
}

// AIDEV-NOTE: Resolve short-lived presigned URLs for the library tracks so the
// native player streams straight from object storage instead of proxying every
// byte through the API (auth + Postgres + storage round-trips per range request).
// Best-effort: any failure yields an empty map and callers fall back to the proxy
// URL (with auth headers). Preview tracks never need resolution.
async function resolveLibraryUrls(
  tracks: readonly PlaybackTrack[],
): Promise<Map<string, string>> {
  const ids: string[] = [];
  for (const t of tracks) {
    if (t.source.kind === 'library') ids.push(t.source.trackId);
    if (ids.length >= MAX_PRESIGN) break;
  }
  if (ids.length === 0) return new Map();
  try {
    const resolved = await fetchAudioUrls(ids);
    return new Map(resolved.map((r) => [r.trackId, r.url]));
  } catch {
    return new Map();
  }
}

// A presigned URL is self-authorizing (the signature rides in the query string),
// so it carries no auth headers; the proxy fallback URL does. Preview tracks
// stream their external URL directly.
function toNativeTrack(
  track: PlaybackTrack,
  headers: Record<string, string>,
  resolved: Map<string, string>,
): AddTrack {
  const artwork = track.artworkUrl ?? '';
  if (track.source.kind === 'preview') {
    return { url: track.source.previewUrl, title: track.title, artist: track.artist, artwork };
  }
  const signed = resolved.get(track.source.trackId);
  if (signed) {
    return { url: signed, title: track.title, artist: track.artist, artwork };
  }
  return {
    url: audioStreamUrl(track.source.trackId),
    title: track.title,
    artist: track.artist,
    artwork,
    headers,
  };
}

export async function loadNativeTrack(
  track: PlaybackTrack,
  options: LoadNativeTrackOptions = {},
): Promise<void> {
  const { autoplay = true, startPositionMs = 0 } = options;

  // AIDEV-WARNING: this reset() drops the native queue, so the caller MUST also
  // clear the queue store (see PlaybackProvider.play) — otherwise the store still
  // describes a queue the player no longer holds, and the add()-induced
  // ActiveTrackChanged(0) repoints the UI at the old queue's first track.
  const token = claimLoad();
  await ensurePlayerSetup();
  if (isStale(token)) return;
  await TrackPlayer.reset();
  if (isStale(token)) return;
  const headers = track.source.kind === 'library' ? await audioRequestHeaders() : {};
  const resolved = await resolveLibraryUrls([track]);
  if (isStale(token)) return;
  await TrackPlayer.add(toNativeTrack(track, headers, resolved));

  if (startPositionMs > 0) {
    await TrackPlayer.seekTo(startPositionMs / 1000);
  }
  if (autoplay) {
    await TrackPlayer.play();
  }
}

// AIDEV-NOTE: Loads the whole ordered queue into the native player in one pass so
// TrackPlayer holds the full play order. Library tracks stream via short-lived
// presigned URLs (resolveLibraryUrls) — direct from storage, no per-byte proxy —
// falling back to the proxy URL on any resolve failure. The native queue mirrors
// play order, so its index == store currentIndex. Auth headers are fetched once
// and reused across proxy-fallback items.
export async function loadNativeQueue(
  tracks: readonly PlaybackTrack[],
  startIndex: number,
  options: LoadNativeTrackOptions = {},
): Promise<void> {
  const { autoplay = true, startPositionMs = 0 } = options;

  const token = claimLoad();
  await ensurePlayerSetup();
  if (isStale(token)) return;
  await TrackPlayer.reset();
  if (tracks.length === 0) return;

  const needsAuth = tracks.some((t) => t.source.kind === 'library');
  const headers = needsAuth ? await audioRequestHeaders() : {};
  // Sign the window from the start index forward — that's what plays next.
  const resolved = await resolveLibraryUrls(tracks.slice(startIndex));
  // A newer load (a user tap beating an in-flight resume) already owns the
  // player — bail before add() so the two don't interleave into one queue.
  if (isStale(token)) return;

  const idx = Math.max(0, Math.min(startIndex, tracks.length - 1));
  // Pin the target index so the add()-induced index-0 transient doesn't flash
  // the wrong track into the store (see nativeSyncGuard). The guard self-clears
  // when the target-index event is applied — we do NOT clear it here on success,
  // because TrackPlayer delivers the event asynchronously (a synchronous clear
  // could lift the guard before the transient is processed). On failure we clear
  // explicitly so a failed prime can't leave the guard pinned.
  beginNativeLoad(idx);
  try {
    await TrackPlayer.add(tracks.map((t) => toNativeTrack(t, headers, resolved)));
    if (idx > 0) await TrackPlayer.skip(idx);
  } catch (err) {
    endNativeLoad();
    throw err;
  }
  if (startPositionMs > 0) await TrackPlayer.seekTo(startPositionMs / 1000);
  if (autoplay) await TrackPlayer.play();
}

// AIDEV-NOTE: Replace only the upcoming tracks (everything after the active
// one) — removeUpcomingTracks + re-add. The currently-playing track is never
// removed, re-added, or reindexed, so audio continues uninterrupted and no
// PlaybackActiveTrackChanged fires. Because only positions after the active
// index change, native index still mirrors the store's play order. Shuffle
// toggles route through here so they're seamless. Presigned URLs are resolved for
// the re-added upcoming items, same as loadNativeQueue.
export async function reorderUpcomingNative(
  upcoming: readonly PlaybackTrack[],
): Promise<void> {
  await ensurePlayerSetup();
  await TrackPlayer.removeUpcomingTracks();
  if (upcoming.length === 0) return;

  const needsAuth = upcoming.some((t) => t.source.kind === 'library');
  const headers = needsAuth ? await audioRequestHeaders() : {};
  const resolved = await resolveLibraryUrls(upcoming);
  await TrackPlayer.add(upcoming.map((t) => toNativeTrack(t, headers, resolved)));
}

// AIDEV-NOTE: Append one track to the end of the native queue (Add to Queue).
// TrackPlayer.add with no insert index appends, which mirrors the store's
// enqueue (new track lands last in play order). The currently-playing track is
// untouched, so audio continues uninterrupted.
export async function appendNativeTrack(track: PlaybackTrack): Promise<void> {
  await ensurePlayerSetup();
  const headers = track.source.kind === 'library' ? await audioRequestHeaders() : {};
  const resolved = await resolveLibraryUrls([track]);
  await TrackPlayer.add(toNativeTrack(track, headers, resolved));
}

// AIDEV-NOTE: Insert one track at `position` in the native queue (Play Next).
// TrackPlayer.add(track, insertBeforeIndex) inserts before that index; passing
// currentIndex+1 places it right after the active track. Native queue position
// == store play-order position, so this stays in lockstep with playNext.
export async function insertNativeTrackNext(
  track: PlaybackTrack,
  position: number,
): Promise<void> {
  await ensurePlayerSetup();
  const headers = track.source.kind === 'library' ? await audioRequestHeaders() : {};
  const resolved = await resolveLibraryUrls([track]);
  await TrackPlayer.add(toNativeTrack(track, headers, resolved), position);
}
