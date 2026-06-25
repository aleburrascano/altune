/**
 * useDiscogsArtistEnrichment — fetch an artist's Discogs detail enrichment.
 *
 * Resolves bio / aliases / groups / external links from the artist name. Gated
 * by the caller to `kind === 'artist'`. An unresolved artist comes back empty
 * and is surfaced as `enrichment: null` so the section hides. Off the search
 * path — one cached call per open (docs/providers/discogs.md cap 7).
 */

import {
  getDiscogsArtistEnrichment,
  type DiscogsArtistEnrichmentResponse,
} from '@shared/api-client/discovery';

import { useEnrichmentQuery } from './useEnrichmentQuery';

type UseDiscogsArtistEnrichmentParams = {
  name: string;
  enabled?: boolean;
};

type UseDiscogsArtistEnrichmentReturn = {
  enrichment: DiscogsArtistEnrichmentResponse | null;
  isLoading: boolean;
  isError: boolean;
};

// hasContent reports whether a payload carries anything worth rendering. An
// unresolved artist (artist_id:0 + empty everything) is treated as "nothing".
function hasContent(e: DiscogsArtistEnrichmentResponse): boolean {
  return (
    e.profile !== '' ||
    e.real_name !== '' ||
    e.aliases.length > 0 ||
    e.name_variations.length > 0 ||
    e.members.length > 0 ||
    e.groups.length > 0 ||
    e.links.length > 0
  );
}

export function useDiscogsArtistEnrichment({
  name,
  enabled = true,
}: UseDiscogsArtistEnrichmentParams): UseDiscogsArtistEnrichmentReturn {
  const { value, isLoading, isError } = useEnrichmentQuery({
    queryKey: ['discogs-artist-enrichment', name],
    queryFn: () => getDiscogsArtistEnrichment({ name }),
    hasContent,
    enabled: enabled && name.trim() !== '',
  });

  return { enrichment: value, isLoading, isError };
}
