import { useQuery } from '@tanstack/react-query';

import { trackExtras } from '../extras-accessors';
import { resolveEntityQuery } from '../resolve-entity-query';
import { useDetailFetchEnabled, useGatedRefetch } from './detailFetchGate';
import { useLoggedSearchFailure } from './useLoggedSearchFailure';

const LASTFM_PLACEHOLDER_HASH = '2a96cbd8b46e442fc41c2b86b821562f';

export function useArtistDiscovery({
  artistName,
  enabled,
}: {
  artistName: string;
  enabled: boolean;
}) {
  const isFetchEnabled = useDetailFetchEnabled();

  const { data, isLoading, isError, error, refetch } = useQuery({
    ...resolveEntityQuery('artist', artistName, 1),
    enabled: enabled && isFetchEnabled,
  });
  const searchResult = data?.[0] ?? null;
  const retrySearch = useGatedRefetch(refetch);

  useLoggedSearchFailure(error, { kind: 'artist', title: artistName, artist: null });

  const rawImageUrl = searchResult?.image_url ?? null;
  const isPlaceholder = rawImageUrl != null && rawImageUrl.includes(LASTFM_PLACEHOLDER_HASH);
  const imageUrl = isPlaceholder ? null : rawImageUrl;

  return {
    artistResult: searchResult,
    imageUrl,
    sources: searchResult?.sources ?? [],
    mbid: searchResult ? trackExtras(searchResult.extras).mbid : null,
    isLoading,
    isError,
    refetch: retrySearch,
  };
}
