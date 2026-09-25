import { markSessionExpired, stampCredentials } from '@shared/auth/sessionExpired';
import { CORRELATION_HEADER, newCorrelationId } from './correlationId';
import { ApiError } from '@shared/errors';
import { idPathSegment, type TrackId } from './ids';
import { apiBase, apiFetch, apiSend, authorization, logFailure } from './index';
import { startDeadline } from './deadline';

// One header set serves every track of a queue load, so the route stands in for the
// track id `audioStreamUrl` would substitute.
const AUDIO_STREAM_ROUTE = '/v1/tracks/{id}/audio';

const SESSION_REJECTED = 401;

function isSessionRefusal(error: unknown): boolean {
  return error instanceof ApiError && error.status === SESSION_REJECTED;
}

export function audioStreamUrl(trackId: TrackId): string {
  return `${apiBase}/v1/tracks/${idPathSegment(trackId)}/audio`;
}

/**
 * Headers for the audio stream, which the native player sends over its own HTTP
 * client rather than through `apiFetch`. Resolves without an `Authorization`
 * header instead of rejecting, because a queue load holding preview or pinned
 * tracks must still proceed.
 */
export async function audioRequestHeaders(): Promise<Record<string, string>> {
  const correlationId = newCorrelationId() ?? undefined;
  return {
    ...(correlationId === undefined ? {} : { [CORRELATION_HEADER]: correlationId }),
    ...(await authorizationHeaderOrNone(correlationId)),
  };
}

/**
 * There is no 401 response for `apiFetch` to read here — the player swallows
 * it — so a session the shared auth path refuses trips the shared expiry signal
 * from this side, and playback that would fail unauthenticated raises the same
 * sign-in prompt every other request does. An unreachable auth server is not an
 * expired session: it leaves the stream to try, and the token to return.
 */
async function authorizationHeaderOrNone(
  correlationId: string | undefined,
): Promise<Record<string, string>> {
  const sentWith = stampCredentials();
  try {
    return { Authorization: await authorization(AUDIO_STREAM_ROUTE, correlationId) };
  } catch (error) {
    if (isSessionRefusal(error)) markSessionExpired(sentWith);
    logFailure('GET', AUDIO_STREAM_ROUTE, correlationId, error);
    return {};
  }
}

export interface ResolvedAudioUrl {
  trackId: string;
  url: string;
  version: string;
}

export async function recoverAudio(trackId: TrackId): Promise<void> {
  await apiFetch<undefined>(`/v1/tracks/${idPathSegment(trackId)}/audio/recover`, {
    method: 'POST',
  });
}

// Remote kill switch for audio prefetching (#824). The server reports its AUDIO_PREFETCH_ENABLED
// setting on every audio-url response; the last value seen wins, so flipping it on the API
// disables (or restores) prefetching in shipped builds without a release. A server that does not
// send the flag leaves prefetching enabled.
let prefetchEnabled = true;

export function isAudioPrefetchEnabled(): boolean {
  return prefetchEnabled;
}

export const AUDIO_URLS_TIMEOUT_MS = 2500;

export async function fetchAudioUrls(trackIds: string[]): Promise<ResolvedAudioUrl[]> {
  if (trackIds.length === 0) return [];

  const deadline = startDeadline(undefined, AUDIO_URLS_TIMEOUT_MS);
  try {
    const data = await apiSend<{
      urls: { track_id: string; url: string; version?: string }[];
      prefetch_enabled?: unknown;
    }>('/v1/audio-urls', 'POST', { track_ids: trackIds }, { signal: deadline.signal });
    if (typeof data.prefetch_enabled === 'boolean') prefetchEnabled = data.prefetch_enabled;
    return data.urls.map((u) => ({ trackId: u.track_id, url: u.url, version: u.version ?? '' }));
  } finally {
    deadline.release();
  }
}
