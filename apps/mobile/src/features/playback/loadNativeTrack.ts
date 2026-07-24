import TrackPlayer, { Event, State } from 'react-native-track-player';

import { pinnedUri } from '@shared/offline/pinnedStore';

import { audioRequestHeaders, fetchAudioUrls } from '@shared/api-client/audio';
import { forgetAllSwaps } from './audioPrefetch';
import { ensurePlayerSetup } from './initPlayer';
import { toNativeTrack } from './nativeTrack';
import { beginNativeLoad, endNativeLoad } from './nativeSyncGuard';
import type { PlaybackTrack } from '@shared/playback/types';

export interface LoadNativeTrackOptions {
  autoplay?: boolean;
  startPositionMs?: number;
}

const MAX_PRESIGN = 25;

let loadToken = 0;

function claimLoad(): number {
  return ++loadToken;
}

function isStale(token: number): boolean {
  return token !== loadToken;
}

type TimingMarks = { start: number; [phase: string]: number };

function reportTiming(scenario: string, marks: TimingMarks): void {
  const { start, ...rest } = marks;
  const offsets = Object.entries(rest)
    .map(([label, t]) => `${label}=${t - start}ms`)
    .join(' ');
  console.log(`[audio-timing] ${scenario} total_ms=${Date.now() - start} ${offsets}`);
}

function logTimeToPlaying(scenario: string, token: number, marks: TimingMarks): void {
  const sub = TrackPlayer.addEventListener(Event.PlaybackState, (e) => {
    if (e.state !== State.Playing || isStale(token)) return;
    clearTimeout(timeout);
    sub.remove();
    reportTiming(scenario, marks);
  });
  const timeout = setTimeout(() => sub.remove(), 15000);
}

async function resolveLibraryUrls(tracks: readonly PlaybackTrack[]): Promise<Map<string, string>> {
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

function signedUrl(track: PlaybackTrack, resolved: Map<string, string>): string | undefined {
  if (track.source.kind !== 'library') return undefined;
  return pinnedUri(track.source.trackId) ?? resolved.get(track.source.trackId);
}

export async function loadNativeTrack(
  track: PlaybackTrack,
  options: LoadNativeTrackOptions = {},
): Promise<void> {
  const { autoplay = true, startPositionMs = 0 } = options;
  const marks: TimingMarks = { start: Date.now() };

  const token = claimLoad();
  await ensurePlayerSetup();
  if (isStale(token)) return;
  await TrackPlayer.reset();
  forgetAllSwaps();
  if (isStale(token)) return;
  const headers = track.source.kind === 'library' ? await audioRequestHeaders() : {};
  marks.headers = Date.now();
  const resolved = await resolveLibraryUrls([track]);
  marks.resolve = Date.now();
  if (isStale(token)) return;
  await TrackPlayer.add(toNativeTrack(track, { streamUrl: signedUrl(track, resolved), headers }));
  marks.added = Date.now();

  if (startPositionMs > 0) {
    await TrackPlayer.seekTo(startPositionMs / 1000);
  }
  if (autoplay) {
    logTimeToPlaying('play', token, marks);
    await TrackPlayer.play();
  }
}

export async function loadNativeQueue(
  tracks: readonly PlaybackTrack[],
  startIndex: number,
  options: LoadNativeTrackOptions = {},
): Promise<void> {
  const { autoplay = true, startPositionMs = 0 } = options;
  const marks: TimingMarks = { start: Date.now() };

  const token = claimLoad();
  await ensurePlayerSetup();
  if (isStale(token)) return;
  await TrackPlayer.reset();
  forgetAllSwaps();
  if (tracks.length === 0) return;

  const needsAuth = tracks.some((t) => t.source.kind === 'library');
  const headers = needsAuth ? await audioRequestHeaders() : {};
  marks.headers = Date.now();
  const resolved = await resolveLibraryUrls(tracks.slice(startIndex));
  marks.resolve = Date.now();
  if (isStale(token)) return;

  const idx = Math.max(0, Math.min(startIndex, tracks.length - 1));
  beginNativeLoad(idx);
  try {
    await TrackPlayer.add(
      tracks.map((t) => toNativeTrack(t, { streamUrl: signedUrl(t, resolved), headers })),
    );
    if (idx > 0) await TrackPlayer.skip(idx);
  } catch (err) {
    endNativeLoad();
    throw err;
  }
  marks.added = Date.now();
  if (startPositionMs > 0) await TrackPlayer.seekTo(startPositionMs / 1000);
  if (autoplay) {
    logTimeToPlaying('queue-start', token, marks);
    await TrackPlayer.play();
  }
}

export async function reorderUpcomingNative(upcoming: readonly PlaybackTrack[]): Promise<void> {
  await ensurePlayerSetup();
  await TrackPlayer.removeUpcomingTracks();
  if (upcoming.length === 0) return;

  const needsAuth = upcoming.some((t) => t.source.kind === 'library');
  const headers = needsAuth ? await audioRequestHeaders() : {};
  const resolved = await resolveLibraryUrls(upcoming);
  await TrackPlayer.add(
    upcoming.map((t) => toNativeTrack(t, { streamUrl: signedUrl(t, resolved), headers })),
  );
}

export async function appendNativeTrack(track: PlaybackTrack): Promise<void> {
  await ensurePlayerSetup();
  const headers = track.source.kind === 'library' ? await audioRequestHeaders() : {};
  const resolved = await resolveLibraryUrls([track]);
  await TrackPlayer.add(toNativeTrack(track, { streamUrl: signedUrl(track, resolved), headers }));
}

export async function insertNativeTrackNext(track: PlaybackTrack, position: number): Promise<void> {
  await ensurePlayerSetup();
  const headers = track.source.kind === 'library' ? await audioRequestHeaders() : {};
  const resolved = await resolveLibraryUrls([track]);
  await TrackPlayer.add(
    toNativeTrack(track, { streamUrl: signedUrl(track, resolved), headers }),
    position,
  );
}
