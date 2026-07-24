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
  musicbrainz: EnrichmentResponse | null;
  deezer: DeezerEnrichmentResponse | null;
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
