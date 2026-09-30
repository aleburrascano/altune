import { fetchAudioUrls } from '@shared/api-client/audio';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { trackKey, type TrackKey } from '@shared/playback/trackKey';
import type { PlaybackSource, PlaybackTrack } from '@shared/playback/types';

import { redactedPlaybackFailure, type RedactedPlaybackFailure } from '../redactPlaybackError';
import {
  isPresignStale,
  type PendingPresign,
  type SourceOutcome,
  type WebAudioPlayer,
} from './webPlayer';

const NO_AUDIO_URL: RedactedPlaybackFailure = {
  kind: 'not_found',
  message: 'No audio is available for this track',
};

function peekNextQueueTrack(): PlaybackTrack | null {
  const state = useQueueStore.getState();
  if (state.repeatMode === 'one') return null;
  const ordered = orderedQueueTracks(state);
  const next = ordered[state.currentIndex + 1];
  if (next) return next;
  return state.repeatMode === 'all' ? (ordered[0] ?? null) : null;
}

function isNextPresignFresh(player: WebAudioPlayer, key: TrackKey): boolean {
  const cached = player.nextPresign;
  return cached !== null && cached.key === key && !isPresignStale(cached.issuedAt, player.now());
}

export async function prefetchNextTrack(player: WebAudioPlayer): Promise<void> {
  const next = peekNextQueueTrack();
  if (!next || next.source.kind === 'preview') return;
  if (isNextPresignFresh(player, trackKey(next))) return;
  const issuedAt = player.now();
  const seq = player.loadSeq;
  const outcome = await resolveSource(next.source);
  if (seq === player.loadSeq && 'url' in outcome)
    player.nextPresign = { key: trackKey(next), url: outcome.url, issuedAt };
}

export function takeNextPresign(
  player: WebAudioPlayer,
  track: PlaybackTrack,
): PendingPresign | null {
  const cached = player.nextPresign;
  if (!cached || cached.key !== trackKey(track)) return null;
  if (isPresignStale(cached.issuedAt, player.now())) return null;
  player.nextPresign = null;
  return cached;
}

export async function resolveSource(source: PlaybackSource): Promise<SourceOutcome> {
  if (source.kind === 'preview') return { url: source.previewUrl };
  try {
    const urls = await fetchAudioUrls([source.trackId]);
    const match = urls.find((resolved) => resolved.trackId === source.trackId);
    return match ? { url: match.url } : { failure: NO_AUDIO_URL };
  } catch (err) {
    return { failure: redactedPlaybackFailure(err) };
  }
}
