/**
 * useDetailEnrichments — the single seam for a detail screen's provider
 * enrichments.
 *
 * Owns the kind → enrichments decision that used to be smeared across
 * DetailScreen: which providers a result of each kind fetches, and the params
 * each needs. The three underlying hooks stay (each fetches a different shape)
 * but they are composed here behind one interface so the screen asks once and
 * reads a content-gated plan. Only fields the screen actually renders live
 * here — the retired Discogs/lyrics surface was deleted, not disabled.
 *
 * React's rules of hooks require all three to be called unconditionally; the
 * kind gating is expressed as each hook's `enabled` flag, not as a conditional
 * call. A disabled or empty enrichment comes back `null`.
 */

import type {
  DeezerEnrichmentResponse,
  EnrichmentResponse,
  LastFmEnrichmentResponse,
} from '@shared/api-client/enrichment';
import type { DiscoveryResult } from '@shared/api-client/discovery';

import { trackExtras } from '../extras-accessors';
import { useDeezerEnrichment } from './useDeezerEnrichment';
import { useEnrichment } from './useEnrichment';
import { useLastFmEnrichment } from './useLastFmEnrichment';

export type DetailEnrichments = {
  /** MusicBrainz — the header year. All kinds. */
  musicbrainz: EnrichmentResponse | null;
  /** Deezer — `featured_artists` for the header collab line + track Featuring row. Track + album. */
  deezer: DeezerEnrichmentResponse | null;
  /** Last.fm — the artist About block (bio, counts, tags, similar). Artist only. */
  lastfm: LastFmEnrichmentResponse | null;
};

export function useDetailEnrichments(result: DiscoveryResult): DetailEnrichments {
  const { kind, title, subtitle } = result;
  const isTrack = kind === 'track';
  const isAlbum = kind === 'album';
  const isArtist = kind === 'artist';
  const mbid = trackExtras(result.extras).mbid ?? undefined;

  const { enrichment: musicbrainz } = useEnrichment({ kind, title, subtitle, mbid });
  const { enrichment: deezer } = useDeezerEnrichment({
    kind,
    title,
    subtitle,
    enabled: isTrack || isAlbum,
  });
  const { enrichment: lastfm } = useLastFmEnrichment({
    kind,
    title,
    subtitle,
    enabled: isArtist,
  });

  return { musicbrainz, deezer, lastfm };
}
