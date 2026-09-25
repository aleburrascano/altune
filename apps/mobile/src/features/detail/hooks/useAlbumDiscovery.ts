import { useQuery } from '@tanstack/react-query';

import { getAlbumTracks } from '@shared/api-client/enrichment';
import type { DiscoveryResult } from '@shared/api-client/discovery';

import { contentFailure, DETAIL_LIST_CAP } from '../content-status';
import { fetchTallyingOutcome } from '../detailHealth';
import { resolveEntityQuery } from '../resolve-entity-query';
import { useDetailFetchEnabled, useGatedRefetch } from './detailFetchGate';
import { useContentFetchRetry } from './useContentFetchRetry';
import { useLoggedSearchFailure } from './useLoggedSearchFailure';

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
  const isFetchEnabled = useDetailFetchEnabled();
  const canFetch = enabled && isFetchEnabled;

  const {
    data,
    isLoading: isSearching,
    isError: isSearchError,
    error: searchError,
    refetch: refetchSearch,
  } = useQuery({
    ...resolveEntityQuery('album', searchQuery, 1),
    enabled: canFetch,
  });
  const searchResult = data?.[0] ?? null;

  useLoggedSearchFailure(searchError, { kind: 'album', title: albumTitle, artist });

  // The search step carries no provider status of its own, so the only reading
  // left is the one its retry affordance needs: settled or worth asking again.
  const searchFailure = contentFailure(isSearchError, searchError, null);

  const source = searchResult?.sources[0];
  const retry = useContentFetchRetry();

  const {
    data: tracksData,
    isLoading: isLoadingTracks,
    isError: isTracksQueryError,
    error: tracksError,
    refetch: refetchTracks,
  } = useQuery({
    queryKey: ['album-discovery-tracks', source?.provider, source?.external_id],
    queryFn: ({ signal }) =>
      fetchTallyingOutcome('album_tracks', () =>
        getAlbumTracks({
          provider: source!.provider,
          externalId: source!.external_id,
          limit: DETAIL_LIST_CAP,
          albumTitle: searchResult?.title,
          albumArtist: searchResult?.subtitle ?? undefined,
          signal,
        }),
      ),
    enabled: canFetch && source != null,
    staleTime: 30 * 60 * 1000,
    retry,
  });

  const tracks: DiscoveryResult[] = tracksData?.items ?? [];
  // A degraded provider status is a failed tracks step, not an album with no
  // more tracks — same reading as every other detail list.
  const tracksFailure = contentFailure(isTracksQueryError, tracksError, tracksData);

  // Either step failing leaves this album's discography with nothing to show,
  // and one retry re-runs both, so callers read one failure whichever step it
  // came from.
  const failure = searchFailure ?? tracksFailure;

  // A retry must re-run whichever step failed. When the search step fails the
  // tracks query is disabled (source is null), so refetching only the tracks
  // query would be a permanent no-op — re-run both so either failure recovers.
  const refetch = useGatedRefetch(() => {
    void refetchSearch();
    void refetchTracks();
  });

  return {
    albumResult: searchResult,
    tracks,
    isLoading: isSearching || isLoadingTracks,
    failure,
    isError: failure !== null,
    refetch,
  };
}
