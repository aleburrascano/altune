import { AppState } from 'react-native';

import { isAbort } from '@shared/errors';
import { recordEvent } from '@shared/telemetry/recordEvent';
import type { DiscoveryProviderStatus } from '@shared/api-client/discovery';

import { hasDegradedStatus } from './content-status';

// Enrichment and detail content fetches degrade silently: a failed provider leaves the same
// empty section an entity with genuinely no data there renders. This tallies outcomes and
// reports them as one aggregate `detail_health` event (never one per fetch), from which a
// per-provider success rate per client batch can be computed without waiting for user
// reports. Same shape and channel as playback's own health tally (features/playback/
// playbackHealth.ts), so both are consumed the same way.

/** The enrichment providers, named as the server's enrichment routes name them. */
export type EnrichmentProvider = 'musicbrainz' | 'deezer' | 'lastfm';

// The detail content fetches, named for the endpoint behind each. Album tracks count under one
// name whether the album arrived from a discovery search or from a known id: one endpoint, one
// provider health.
export type ContentFetch = 'album_tracks' | 'artist_content' | 'related_tracks';

// Outcomes per reported batch; the batch also flushes when the app goes to the background.
export const DETAIL_HEALTH_BATCH = 25;

type Tally = {
  enrichment_musicbrainz_ok: number;
  enrichment_musicbrainz_failed: number;
  enrichment_deezer_ok: number;
  enrichment_deezer_failed: number;
  enrichment_lastfm_ok: number;
  enrichment_lastfm_failed: number;
  content_album_tracks_ok: number;
  content_album_tracks_failed: number;
  content_artist_content_ok: number;
  content_artist_content_failed: number;
  content_related_tracks_ok: number;
  content_related_tracks_failed: number;
};

const emptyTally = (): Tally => ({
  enrichment_musicbrainz_ok: 0,
  enrichment_musicbrainz_failed: 0,
  enrichment_deezer_ok: 0,
  enrichment_deezer_failed: 0,
  enrichment_lastfm_ok: 0,
  enrichment_lastfm_failed: 0,
  content_album_tracks_ok: 0,
  content_album_tracks_failed: 0,
  content_artist_content_ok: 0,
  content_artist_content_failed: 0,
  content_related_tracks_ok: 0,
  content_related_tracks_failed: 0,
});

let tally = emptyTally();
let outcomes = 0;
let listening = false;

function count(key: keyof Tally): void {
  ensureFlushOnBackground();
  tally[key] += 1;
  outcomes += 1;
  if (outcomes >= DETAIL_HEALTH_BATCH) flushDetailHealth();
}

export function recordEnrichmentOutcome(provider: EnrichmentProvider, ok: boolean): void {
  count(`enrichment_${provider}_${ok ? 'ok' : 'failed'}`);
}

export function recordContentFetchOutcome(fetch: ContentFetch, ok: boolean): void {
  count(`content_${fetch}_${ok ? 'ok' : 'failed'}`);
}

/**
 * Runs one detail content fetch and tallies its outcome. A degraded provider status counts as
 * a failure, the reading the retry UI already renders; an aborted fetch counts as neither, so
 * a screen the user navigated away from cannot read as provider degradation.
 */
export async function fetchTallyingOutcome<T extends { status: DiscoveryProviderStatus }>(
  fetch: ContentFetch,
  run: () => Promise<T>,
): Promise<T> {
  try {
    const response = await run();
    recordContentFetchOutcome(fetch, !hasDegradedStatus(response));
    return response;
  } catch (error) {
    if (!isAbort(error)) recordContentFetchOutcome(fetch, false);
    throw error;
  }
}

// Best effort: a batch that fails to send is dropped, since a health sample is not worth an
// outbox slot the label-critical events need.
export function flushDetailHealth(): void {
  if (outcomes === 0) return;
  const payload = tally;
  tally = emptyTally();
  outcomes = 0;
  recordEvent({ type: 'detail_health', payload }).catch(() => undefined);
}

function ensureFlushOnBackground(): void {
  if (listening) return;
  listening = true;
  AppState.addEventListener('change', (status) => {
    if (status === 'background') flushDetailHealth();
  });
}

export function _resetDetailHealthForTest(): void {
  tally = emptyTally();
  outcomes = 0;
}
