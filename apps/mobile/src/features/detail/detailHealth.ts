import { AppState } from 'react-native';

import { isAbort } from '@shared/errors';
import { recordEvent } from '@shared/telemetry/recordEvent';
import type { DiscoveryProviderStatus } from '@shared/api-client/discovery';

import { hasDegradedStatus } from './content-status';

export type EnrichmentProvider = 'musicbrainz' | 'deezer' | 'lastfm';

export type ContentFetch = 'album_tracks' | 'artist_content' | 'related_tracks';

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
