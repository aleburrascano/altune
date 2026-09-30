import { resolvePinnedUri } from '@shared/offline/pinnedStore';
import type { AddTrack } from 'react-native-track-player';

import {
  audioRequestHeaders,
  fetchAudioUrls,
  type ResolvedAudioUrl,
} from '@shared/api-client/audio';
import { classifyPlaybackFailure } from '../classifyPlaybackError';
import { warnPlayback } from '../redactPlaybackError';
import { recordPresignOutcome } from '../playbackHealth';
import { MAX_PRESIGN } from '../presignWindow';
import { toNativeTrack } from './nativeTrack';
import type { PlaybackTrack } from '@shared/playback/types';

function headersFor(tracks: readonly PlaybackTrack[]): Promise<Record<string, string>> {
  const needsAuth = tracks.some((t) => t.source.kind === 'library');
  return needsAuth ? audioRequestHeaders() : Promise.resolve({});
}

interface ResolvedUrls {
  urls: Map<string, ResolvedAudioUrl>;
  denied: boolean;
}

function libraryIdsToSign(tracks: readonly PlaybackTrack[]): string[] {
  const ids: string[] = [];
  for (const t of tracks) {
    if (t.source.kind === 'library') ids.push(t.source.trackId);
    if (ids.length >= MAX_PRESIGN) break;
  }
  return ids;
}

function presignFailed(ids: string[], err: unknown): ResolvedUrls {
  warnPlayback('presign failed', { trackIds: ids }, err);
  recordPresignOutcome(false);
  return { urls: new Map(), denied: classifyPlaybackFailure(err) === 'auth' };
}

async function fetchResolvedUrls(ids: string[]): Promise<ResolvedUrls> {
  try {
    const resolved = await fetchAudioUrls(ids);
    recordPresignOutcome(true);
    return { urls: new Map(resolved.map((r) => [r.trackId, r])), denied: false };
  } catch (err) {
    return presignFailed(ids, err);
  }
}

async function resolveLibraryUrls(tracks: readonly PlaybackTrack[]): Promise<ResolvedUrls> {
  const ids = libraryIdsToSign(tracks);
  if (ids.length === 0) return { urls: new Map(), denied: false };
  return fetchResolvedUrls(ids);
}

function signedUrl(track: PlaybackTrack, resolved: ResolvedUrls): string | undefined {
  if (track.source.kind !== 'library' || resolved.denied) return undefined;
  const match = resolved.urls.get(track.source.trackId);
  return resolvePinnedUri(track.source.trackId, match?.version) ?? match?.url;
}

export async function nativeTrackBuilder(
  signFor: readonly PlaybackTrack[],
  headersForTracks: readonly PlaybackTrack[] = signFor,
): Promise<(track: PlaybackTrack) => AddTrack> {
  const headers = await headersFor(headersForTracks);
  const resolved = await resolveLibraryUrls(signFor);
  return (track) => toNativeTrack(track, { streamUrl: signedUrl(track, resolved), headers });
}
