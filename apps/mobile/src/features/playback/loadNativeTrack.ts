import TrackPlayer, { type AddTrack } from 'react-native-track-player';

import { pinnedUri, repinIfStale } from '@shared/offline/pinnedStore';

import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import {
  audioRequestHeaders,
  fetchAudioUrls,
  type ResolvedAudioUrl,
} from '@shared/api-client/audio';
import { forgetAllSwaps } from './audioPrefetch';
import { ensurePlayerSetup } from './initPlayer';
import { withNativeQueue } from './nativeQueueLock';
import { toNativeTrack } from './nativeTrack';
import { beginNativeLoad, endNativeLoad } from './nativeSyncGuard';
import type { PlaybackTrack } from '@shared/playback/types';

export interface LoadNativeTrackOptions {
  autoplay?: boolean;
  startPositionMs?: number;
}

const MAX_PRESIGN = 25;
// Re-presign the upcoming window once the queue has advanced to within this many
// tracks of the edge of the currently presigned block, so a long shuffle session
// never runs off the end of the initial MAX_PRESIGN signed URLs.
const PRESIGN_REFRESH_MARGIN = 5;

// Highest queue position (in playOrder space) whose native URL we have presigned.
// Tracked so the presign window can slide forward as playback advances instead of
// staying pinned to the first MAX_PRESIGN tracks loaded at queue start.
let presignedThrough = -1;

function markPresignedFrom(startIndex: number, available: number): void {
  presignedThrough = startIndex + Math.min(MAX_PRESIGN, available) - 1;
}

let loadToken = 0;

function claimLoad(): number {
  return ++loadToken;
}

function isStale(token: number): boolean {
  return token !== loadToken;
}

async function resolveLibraryUrls(
  tracks: readonly PlaybackTrack[],
): Promise<Map<string, ResolvedAudioUrl>> {
  const ids: string[] = [];
  for (const t of tracks) {
    if (t.source.kind === 'library') ids.push(t.source.trackId);
    if (ids.length >= MAX_PRESIGN) break;
  }
  if (ids.length === 0) return new Map();
  try {
    const resolved = await fetchAudioUrls(ids);
    return new Map(resolved.map((r) => [r.trackId, r]));
  } catch {
    return new Map();
  }
}

function signedUrl(
  track: PlaybackTrack,
  resolved: Map<string, ResolvedAudioUrl>,
): string | undefined {
  if (track.source.kind !== 'library') return undefined;
  const match = resolved.get(track.source.trackId);
  repinIfStale(track.source.trackId, match?.version);
  return pinnedUri(track.source.trackId, match?.version) ?? match?.url;
}

export async function loadNativeTrack(
  track: PlaybackTrack,
  options: LoadNativeTrackOptions = {},
): Promise<void> {
  const { autoplay = true, startPositionMs = 0 } = options;

  const token = claimLoad();
  await ensurePlayerSetup();
  if (isStale(token)) return;
  await resetNative();
  if (isStale(token)) return;
  const headers = track.source.kind === 'library' ? await audioRequestHeaders() : {};
  const resolved = await resolveLibraryUrls([track]);
  if (isStale(token)) return;

  await withNativeQueue(async () => {
    if (isStale(token)) return;
    await TrackPlayer.add(toNativeTrack(track, { streamUrl: signedUrl(track, resolved), headers }));
    if (startPositionMs > 0) {
      await TrackPlayer.seekTo(startPositionMs / 1000);
    }
    if (autoplay) {
      await TrackPlayer.play();
    }
  });
}

function resetNative(): Promise<void> {
  return withNativeQueue(async () => {
    await TrackPlayer.reset();
    forgetAllSwaps();
  });
}

export async function loadNativeQueue(
  tracks: readonly PlaybackTrack[],
  startIndex: number,
  options: LoadNativeTrackOptions = {},
): Promise<void> {
  const { autoplay = true, startPositionMs = 0 } = options;

  const token = claimLoad();
  await ensurePlayerSetup();
  if (isStale(token)) return;
  await resetNative();
  if (tracks.length === 0) return;

  const needsAuth = tracks.some((t) => t.source.kind === 'library');
  const headers = needsAuth ? await audioRequestHeaders() : {};
  const resolved = await resolveLibraryUrls(tracks.slice(startIndex));
  if (isStale(token)) return;

  const idx = Math.max(0, Math.min(startIndex, tracks.length - 1));
  markPresignedFrom(idx, tracks.length - idx);
  await withNativeQueue(async () => {
    if (isStale(token)) return;
    const generation = beginNativeLoad(idx);
    try {
      await TrackPlayer.add(
        tracks.map((t) => toNativeTrack(t, { streamUrl: signedUrl(t, resolved), headers })),
      );
      if (idx > 0) await TrackPlayer.skip(idx);
      if (startPositionMs > 0) await TrackPlayer.seekTo(startPositionMs / 1000);
      if (autoplay) {
        await TrackPlayer.play();
      }
    } finally {
      endNativeLoad(generation);
    }
  });
}

export async function reorderUpcomingNative(upcoming: readonly PlaybackTrack[]): Promise<void> {
  await ensurePlayerSetup();
  const needsAuth = upcoming.some((t) => t.source.kind === 'library');
  const headers = needsAuth ? await audioRequestHeaders() : {};
  const resolved = await resolveLibraryUrls(upcoming);
  await withNativeQueue(async () => {
    await TrackPlayer.removeUpcomingTracks();
    if (upcoming.length === 0) return;
    await TrackPlayer.add(
      upcoming.map((t) => toNativeTrack(t, { streamUrl: signedUrl(t, resolved), headers })),
    );
  });
}

// Slide the presign window forward as the queue advances. The native queue may
// hold hundreds of tracks but only MAX_PRESIGN of them carry a fresh signed URL;
// left alone, a long shuffle session eventually reaches unsigned tracks and
// stalls or repeats. When the active track nears the edge of the presigned block,
// re-presign the next window of upcoming tracks from the current position.
export async function refreshUpcomingPresign(currentIndex: number): Promise<void> {
  if (currentIndex < 0) return;
  if (presignedThrough - currentIndex > PRESIGN_REFRESH_MARGIN) return;
  const s = useQueueStore.getState();
  const upcoming = orderedQueueTracks(s).slice(currentIndex + 1);
  if (upcoming.length === 0) return;
  markPresignedFrom(currentIndex + 1, upcoming.length);
  await reorderUpcomingNative(upcoming);
}

export async function appendNativeTrack(track: PlaybackTrack): Promise<void> {
  const native = await resolveNative(track);
  await withNativeQueue(() => TrackPlayer.add(native));
}

export async function insertNativeTrackNext(track: PlaybackTrack, position: number): Promise<void> {
  const native = await resolveNative(track);
  await withNativeQueue(() => TrackPlayer.add(native, position));
}

async function resolveNative(track: PlaybackTrack): Promise<AddTrack> {
  await ensurePlayerSetup();
  const headers = track.source.kind === 'library' ? await audioRequestHeaders() : {};
  const resolved = await resolveLibraryUrls([track]);
  return toNativeTrack(track, { streamUrl: signedUrl(track, resolved), headers });
}
