/**
 * useLastFmEnrichment — fetch an entity's Last.fm detail enrichment on open.
 *
 * Resolves listen-based popularity (listeners/playcount), weighted tags, a
 * bio/blurb, and (for artists) similar artists from the result's kind + title +
 * subtitle. An unresolved entity comes back empty and is surfaced as
 * `enrichment: null` so the section hides. Off the search path — one cached call
 * per open (docs/providers/lastfm.md cap 3).
 */

import {
  getLastFmEnrichment,
  type DiscoveryKind,
  type LastFmEnrichmentResponse,
} from '@shared/api-client/discovery';

import { useEnrichmentQuery } from './useEnrichmentQuery';

type UseLastFmEnrichmentParams = {
  kind: DiscoveryKind;
  title: string;
  subtitle?: string | null | undefined;
  enabled?: boolean;
};

type UseLastFmEnrichmentReturn = {
  enrichment: LastFmEnrichmentResponse | null;
  isLoading: boolean;
  isError: boolean;
};

// hasContent reports whether a payload carries anything worth rendering. An
// unresolved entity (mbid:"" + zero counts + empty everything) is "nothing".
function hasContent(e: LastFmEnrichmentResponse): boolean {
  return (
    e.listeners > 0 ||
    e.playcount > 0 ||
    e.tags.length > 0 ||
    e.bio !== '' ||
    e.similar.length > 0
  );
}

export function useLastFmEnrichment({
  kind,
  title,
  subtitle,
  enabled = true,
}: UseLastFmEnrichmentParams): UseLastFmEnrichmentReturn {
  const { value, isLoading, isError } = useEnrichmentQuery({
    queryKey: ['lastfm-enrichment', kind, `${title}|${subtitle ?? ''}`],
    queryFn: () => getLastFmEnrichment({ kind, title, subtitle }),
    hasContent,
    enabled: enabled && title.trim() !== '',
  });

  return { enrichment: value, isLoading, isError };
}
