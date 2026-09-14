import TrackPlayer, { type AddTrack } from 'react-native-track-player';

import { pinnedUri, repinIfStale } from '@shared/offline/pinnedStore';

import {
  audioRequestHeaders,
  fetchAudioUrls,
  type ResolvedAudioUrl,
} from '@shared/api-client/audio';
import { forgetAllSwaps } from './audioPrefetch';
import { ensurePlayerSetup } from './initPlayer';
import { withNativeQueue } from './nativeQueueLock';
import { toNativeTrack } from './nativeTrack';
import { claimLoad, isStale } from './loadToken';
import { beginNativeLoad, endNativeLoad } from './nativeSyncGuard';
import {
  MAX_PRESIGN,
  markPresignedFrom,
  refreshUpcomingPresign as slidePresignWindow,
} from './presignWindow';
import type { PlaybackTrack } from '@shared/playback/types';

export interface LoadNativeTrackOptions {
  autoplay?: boolean;
  startPositionMs?: number;
}

function headersFor(tracks: readonly PlaybackTrack[]): Promise<Record<string, string>> {
  const needsAuth = tracks.some((t) => t.source.kind === 'library');
  return needsAuth ? audioRequestHeaders() : Promise.resolve({});
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
  const headers = await headersFor([track]);
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

  const headers = await headersFor(tracks);
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
  const headers = await headersFor(upcoming);
  const resolved = await resolveLibraryUrls(upcoming);
  await withNativeQueue(async () => {
    await TrackPlayer.removeUpcomingTracks();
    if (upcoming.length === 0) return;
    await TrackPlayer.add(
      upcoming.map((t) => toNativeTrack(t, { streamUrl: signedUrl(t, resolved), headers })),
    );
  });
}

// The presign-window policy lives in ./presignWindow; this binds it to the native
// reorder so callers keep a single-argument entry point.
export function refreshUpcomingPresign(currentIndex: number): Promise<void> {
  return slidePresignWindow(currentIndex, reorderUpcomingNative);
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
  const headers = await headersFor([track]);
  const resolved = await resolveLibraryUrls([track]);
  return toNativeTrack(track, { streamUrl: signedUrl(track, resolved), headers });
}
