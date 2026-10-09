import type {
  DeezerEnrichmentResponse,
  EnrichmentResponse,
  LastFmEnrichmentResponse,
} from '@shared/api-client/enrichment';
import type { DiscoveryResult } from '@shared/api-client/discovery';

import type { EnrichmentProvider } from '@/features/detail/domain/detailHealth';
import { trackExtras } from '@/features/detail/domain/extras-accessors';
import { useDeezerEnrichment } from '@/features/detail/hooks/useDeezerEnrichment';
import { useEnrichment } from '@/features/detail/hooks/useEnrichment';
import { useLastFmEnrichment } from '@/features/detail/hooks/useLastFmEnrichment';

export type EnrichmentErrors = Record<EnrichmentProvider, boolean>;

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
