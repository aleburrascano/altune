import { useQuery } from '@tanstack/react-query';

import { getAlbumTracks } from '@shared/api-client/enrichment';
import type { DiscoveryResult, DiscoverySource } from '@shared/api-client/discovery';

import { isContentError } from '../content-status';
import { useContentFetchRetry } from './useContentFetchRetry';

type UseAlbumTracksParams = {
  provider: string;
  externalId: string;
  albumTitle?: string;
  albumArtist?: string | undefined;
  allSources?: DiscoverySource[];
  enabled?: boolean;
};

type UseAlbumTracksReturn = {
  tracks: DiscoveryResult[];
  isLoading: boolean;
  isError: boolean;
  refetch: () => void;
};

// Bound oversized tracklists (e.g. box sets) so a single album detail fetch
// cannot pull an unbounded payload. Matches the artist albums cap.
const ALBUM_TRACKS_LIMIT = 100;

export function useAlbumTracks({
  provider,
  externalId,
  albumTitle,
  albumArtist,
  allSources,
  enabled = true,
}: UseAlbumTracksParams): UseAlbumTracksReturn {
  const mbExternalId = allSources?.find((s) => s.provider === 'musicbrainz')?.external_id;
  const retry = useContentFetchRetry();

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['album-tracks', provider, externalId, mbExternalId ?? ''],
    queryFn: ({ signal }) =>
      getAlbumTracks(
        provider,
        externalId,
        ALBUM_TRACKS_LIMIT,
        albumTitle,
        albumArtist,
        mbExternalId,
        signal,
      ),
    enabled,
    staleTime: 1000 * 60 * 30,
    retry,
  });

  return {
    tracks: data?.items ?? [],
    isLoading,
    isError: isContentError(isError, data),
    refetch: () => {
      void refetch();
    },
  };
}
