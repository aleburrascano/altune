import TrackPlayer, { type AddTrack } from 'react-native-track-player';

import type { ResolvedAudioUrl } from '@shared/apiClient';
import { resolvePinnedUri } from '@shared/offline';
import {
  type PlaybackTrack,
  trackKey,
  type TrackKey,
  isPlayablePreviewUrl,
} from '@shared/playback';
import { playbackPort } from '@features/player/ports/activePlaybackPort';
import { artworkPlaceholderUri } from '@features/player/ports/artworkPlaceholder';
import { classifyPlaybackFailure, warnPlayback } from '@features/player/core/playbackError';
import { recordPresignFailed, recordPresignOk } from '@features/player/core/playbackHealth';
import { libraryTrackId } from '@features/player/core/playerStatus';
import { MAX_PRESIGN } from '@features/player/core/presignWindow';
import { ContractError } from '@shared/failure';

function playablePreviewUrl(previewUrl: string): string {
  if (!isPlayablePreviewUrl(previewUrl)) throw new ContractError('PreviewUrl', 'not an https url');
  return previewUrl;
}

export function toNativeTrack(
  track: PlaybackTrack,
  opts: { streamUrl?: string | undefined; headers?: Record<string, string> | undefined } = {},
): AddTrack {
  const base = {
    id: trackKey(track),
    title: track.title,
    artist: track.artist,
    artwork: track.artworkUrl ?? artworkPlaceholderUri(),
  };
  if (opts.streamUrl) return { ...base, url: opts.streamUrl };
  if (track.source.kind === 'preview') {
    return { ...base, url: playablePreviewUrl(track.source.previewUrl) };
  }
  return {
    ...base,
    url: playbackPort().audioStreamUrl(track.source.trackId),
    headers: opts.headers ?? {},
  };
}

export function activeNativeTrackId(): Promise<TrackKey | undefined> {
  return TrackPlayer.getActiveTrack().then(
    (active) => (typeof active?.id === 'string' ? (active.id as TrackKey) : undefined),
    () => undefined,
  );
}

function headersFor(tracks: readonly PlaybackTrack[]): Promise<Record<string, string>> {
  const needsAuth = tracks.some((t) => t.source.kind === 'library');
  return needsAuth ? playbackPort().audioRequestHeaders() : Promise.resolve({});
}

interface ResolvedUrls {
  urls: Map<string, ResolvedAudioUrl>;
  denied: boolean;
  presigned: boolean;
}

function libraryIdsToSign(tracks: readonly PlaybackTrack[]): string[] {
  const ids: string[] = [];
  for (const t of tracks) {
    const id = libraryTrackId(t);
    if (id !== undefined) ids.push(id);
    if (ids.length >= MAX_PRESIGN) break;
  }
  return ids;
}

function presignFailed(ids: string[], err: unknown): ResolvedUrls {
  warnPlayback('presign failed', { trackIds: ids }, err);
  recordPresignFailed();
  return { urls: new Map(), denied: classifyPlaybackFailure(err) === 'auth', presigned: false };
}

async function fetchResolvedUrls(ids: string[]): Promise<ResolvedUrls> {
  try {
    const resolved = await playbackPort().fetchAudioUrls(ids);
    recordPresignOk();
    return { urls: new Map(resolved.map((r) => [r.trackId, r])), denied: false, presigned: true };
  } catch (err) {
    return presignFailed(ids, err);
  }
}

async function resolveLibraryUrls(tracks: readonly PlaybackTrack[]): Promise<ResolvedUrls> {
  const ids = libraryIdsToSign(tracks);
  if (ids.length === 0) return { urls: new Map(), denied: false, presigned: true };
  return fetchResolvedUrls(ids);
}

function signedUrl(track: PlaybackTrack, resolved: ResolvedUrls): string | undefined {
  if (track.source.kind !== 'library' || resolved.denied) return undefined;
  const match = resolved.urls.get(track.source.trackId);
  return resolvePinnedUri(track.source.trackId, match?.version) ?? match?.url;
}

interface NativeTrackBuild {
  build: (track: PlaybackTrack) => AddTrack;
  presigned: boolean;
}

export async function nativeTrackBuilder(
  signFor: readonly PlaybackTrack[],
  headersForTracks: readonly PlaybackTrack[] = signFor,
): Promise<NativeTrackBuild> {
  const [headers, resolved] = await Promise.all([
    headersFor(headersForTracks),
    resolveLibraryUrls(signFor),
  ]);
  return {
    build: (track) => toNativeTrack(track, { streamUrl: signedUrl(track, resolved), headers }),
    presigned: resolved.presigned,
  };
}
