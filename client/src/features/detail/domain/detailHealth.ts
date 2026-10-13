import { isAbort } from '@shared/failure/errors';
import { HEALTH_BATCH, createHealthTally } from '@shared/telemetry/healthTally';
import type { DiscoveryProviderStatus } from '@shared/apiClient/discovery';

import { hasDegradedStatus } from '@features/detail/domain/contentStatus';

export type EnrichmentProvider = 'musicbrainz' | 'deezer' | 'lastfm';

export type ContentFetch = 'album_tracks' | 'artist_content' | 'related_tracks';

const HEALTH_KEYS = [
  'enrichment_musicbrainz_ok',
  'enrichment_musicbrainz_failed',
  'enrichment_deezer_ok',
  'enrichment_deezer_failed',
  'enrichment_lastfm_ok',
  'enrichment_lastfm_failed',
  'content_album_tracks_ok',
  'content_album_tracks_failed',
  'content_artist_content_ok',
  'content_artist_content_failed',
  'content_related_tracks_ok',
  'content_related_tracks_failed',
] as const;

type HealthKey = (typeof HEALTH_KEYS)[number];

const health = createHealthTally<HealthKey>({
  eventType: 'detail_health',
  keys: HEALTH_KEYS,
  onFlushFailure: (error: unknown) => {
    console.warn('[detail] health flush failed', { error });
  },
});

export const DETAIL_HEALTH_BATCH = HEALTH_BATCH;

const { count } = health;

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

export const flushDetailHealth = health.flush;

export const _resetDetailHealthForTest = health.reset;
