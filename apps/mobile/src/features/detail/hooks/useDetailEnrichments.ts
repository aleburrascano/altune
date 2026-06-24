/**
 * useDetailEnrichments — the single seam for a detail screen's provider
 * enrichments.
 *
 * Owns the kind → enrichments decision that used to be smeared across
 * DetailScreen: which providers a result of each kind fetches, and the params
 * each needs. The six underlying hooks stay (each fetches a different shape and
 * renders a different section), but they are composed here behind one interface
 * so the screen asks once and reads a content-gated plan.
 *
 * React's rules of hooks require all six to be called unconditionally; the
 * kind gating is expressed as each hook's `enabled` flag, not as a conditional
 * call. A disabled or empty enrichment comes back `null` so its section hides.
 */

import type {
  DiscoveryResult,
  DeezerEnrichmentResponse,
  DiscogsArtistEnrichmentResponse,
  DiscogsEnrichmentResponse,
  EnrichmentResponse,
  LastFmEnrichmentResponse,
  LyricsResponse,
} from '@shared/api-client/discovery';

import { trackExtras } from '../extras-accessors';
import { useDeezerEnrichment } from './useDeezerEnrichment';
import { useDiscogsArtistEnrichment } from './useDiscogsArtistEnrichment';
import { useDiscogsEnrichment } from './useDiscogsEnrichment';
import { useEnrichment } from './useEnrichment';
import { useLastFmEnrichment } from './useLastFmEnrichment';
import { useLyrics } from './useLyrics';

export type DetailEnrichments = {
  /** MusicBrainz — genres / year / rating + HD cover. All kinds. */
  musicbrainz: EnrichmentResponse | null;
  /** Deezer — track tempo/explicit or album label/genres. Track + album. */
  deezer: DeezerEnrichmentResponse | null;
  /** Last.fm — listen popularity, tags, similar artists, bio. All kinds. */
  lastfm: LastFmEnrichmentResponse | null;
  /** Discogs — album credits / styles / labels / community. Album only. */
  discogsAlbum: DiscogsEnrichmentResponse | null;
  /** Discogs — artist bio / aliases / groups / links. Artist only. */
  discogsArtist: DiscogsArtistEnrichmentResponse | null;
  /** Deezer — synced + plain lyrics. Track only. */
  lyrics: LyricsResponse | null;
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
  const { enrichment: lastfm } = useLastFmEnrichment({ kind, title, subtitle });
  const { enrichment: discogsAlbum } = useDiscogsEnrichment({
    album: title,
    artist: subtitle,
    enabled: isAlbum,
  });
  const { enrichment: discogsArtist } = useDiscogsArtistEnrichment({
    name: title,
    enabled: isArtist,
  });
  const { lyrics } = useLyrics({ title, subtitle, enabled: isTrack });

  return { musicbrainz, deezer, lastfm, discogsAlbum, discogsArtist, lyrics };
}
