import { useQuery } from '@tanstack/react-query';

import { getAlbumTracks } from '@shared/api-client/enrichment';
import type { DiscoveryResult } from '@shared/api-client/discovery';

import { isContentError } from '../content-status';
import { resolveEntityQuery } from '../resolve-entity-query';
import { useContentFetchRetry } from './useContentFetchRetry';

// Bound oversized tracklists so a discovery-driven album fetch stays capped,
// consistent with useAlbumTracks and the artist albums limit.
const ALBUM_TRACKS_LIMIT = 100;

export function useAlbumDiscovery({
  albumTitle,
  artist,
  enabled,
}: {
  albumTitle: string;
  artist: string | null;
  enabled: boolean;
}) {
  const searchQuery = `${albumTitle} ${artist ?? ''}`.trim();

  const {
    data,
    isLoading: isSearching,
    isError: isSearchError,
    refetch: refetchSearch,
  } = useQuery({
    ...resolveEntityQuery('album', searchQuery, 1),
    enabled,
  });
  const searchResult = data?.[0] ?? null;

  const source = searchResult?.sources[0];
  const retry = useContentFetchRetry();

  const {
    data: tracksData,
    isLoading: isLoadingTracks,
    isError: isTracksQueryError,
    refetch: refetchTracks,
  } = useQuery({
    queryKey: ['album-discovery-tracks', source?.provider, source?.external_id],
    queryFn: ({ signal }) =>
      getAlbumTracks(
        source!.provider,
        source!.external_id,
        ALBUM_TRACKS_LIMIT,
        searchResult?.title,
        searchResult?.subtitle ?? undefined,
        undefined,
        signal,
      ),
    enabled: enabled && source != null,
    staleTime: 30 * 60 * 1000,
    retry,
  });

  const tracks: DiscoveryResult[] = tracksData?.items ?? [];
  // A degraded provider status is a failed tracks step, not an album with no
  // more tracks — same reading as every other detail list.
  const isTracksError = isContentError(isTracksQueryError, tracksData);

  // A retry must re-run whichever step failed. When the search step fails the
  // tracks query is disabled (source is null), so refetching only the tracks
  // query would be a permanent no-op — re-run both so either failure recovers.
  const refetch = (): void => {
    void refetchSearch();
    void refetchTracks();
  };

  return {
    albumResult: searchResult,
    tracks,
    isLoading: isSearching || isLoadingTracks,
    // Kept as separate signals so callers can tell "couldn't find this album"
    // (search) apart from "found it but couldn't list its tracks" (tracks).
    isSearchError,
    isTracksError,
    isError: isSearchError || isTracksError,
    refetch,
  };
}
