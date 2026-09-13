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

export type EnrichmentErrors = {
  musicbrainz: boolean;
  deezer: boolean;
  lastfm: boolean;
};

export type DetailEnrichments = {
  musicbrainz: EnrichmentResponse | null;
  deezer: DeezerEnrichmentResponse | null;
  lastfm: LastFmEnrichmentResponse | null;
  errors: EnrichmentErrors;
};

export function useDetailEnrichments(result: DiscoveryResult): DetailEnrichments {
  const { kind, title, subtitle } = result;
  const isTrack = kind === 'track';
  const isAlbum = kind === 'album';
  const isArtist = kind === 'artist';
  const mbid = trackExtras(result.extras).mbid ?? undefined;

  const mb = useEnrichment({ kind, title, subtitle, mbid });
  const dz = useDeezerEnrichment({ kind, title, subtitle, enabled: isTrack || isAlbum });
  const lf = useLastFmEnrichment({ kind, title, subtitle, enabled: isArtist });

  return {
    musicbrainz: mb.enrichment,
    deezer: dz.enrichment,
    lastfm: lf.enrichment,
    errors: { musicbrainz: mb.isError, deezer: dz.isError, lastfm: lf.isError },
  };
}
