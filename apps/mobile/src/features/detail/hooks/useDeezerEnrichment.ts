/**
 * useDeezerEnrichment — fetch a track/album's Deezer detail enrichment on open.
 *
 * Resolves the track audio fields (BPM, explicit flag) or album liner data
 * (label, genres) from the result's kind + title + subtitle. An unresolved
 * entity comes back empty and is surfaced as `enrichment: null` so the section
 * hides. Gated to track/album (artist has no Deezer detail surface here). Off the
 * search path — one cached call per open (docs/providers/deezer.md caps 7–8).
 */

import {
  getDeezerEnrichment,
  type DeezerEnrichmentResponse,
  type DiscoveryKind,
} from '@shared/api-client/discovery';

import { useEnrichmentQuery } from './useEnrichmentQuery';

type UseDeezerEnrichmentParams = {
  kind: DiscoveryKind;
  title: string;
  subtitle?: string | null | undefined;
  enabled?: boolean;
};

type UseDeezerEnrichmentReturn = {
  enrichment: DeezerEnrichmentResponse | null;
  isLoading: boolean;
  isError: boolean;
};

// hasContent reports whether a payload carries anything worth rendering. `gain`
// is excluded — it is a volume-normalization value, never displayed.
function hasContent(e: DeezerEnrichmentResponse): boolean {
  return (
    e.bpm > 0 ||
    e.explicit ||
    e.label !== '' ||
    e.genres.length > 0 ||
    e.record_type !== ''
  );
}

export function useDeezerEnrichment({
  kind,
  title,
  subtitle,
  enabled = true,
}: UseDeezerEnrichmentParams): UseDeezerEnrichmentReturn {
  const { value, isLoading, isError } = useEnrichmentQuery({
    queryKey: ['deezer-enrichment', kind, `${title}|${subtitle ?? ''}`],
    queryFn: () => getDeezerEnrichment({ kind, title, subtitle }),
    hasContent,
    enabled: enabled && title.trim() !== '',
  });

  return { enrichment: value, isLoading, isError };
}
