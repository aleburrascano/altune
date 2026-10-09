import { useQuery } from '@tanstack/react-query';

import { resolveEntityQuery } from '@/features/detail/domain/resolveEntityQuery';
import { useDetailPort } from '@/features/detail/ports/DetailPortProvider';
import { useDetailFetchEnabled } from '@/features/detail/hooks/useDetailFetchEnabled';
import { useGatedRefetch } from '@/features/detail/hooks/useGatedRefetch';
import { useLoggedSearchFailure } from '@/features/detail/hooks/useLoggedSearchFailure';

const LASTFM_PLACEHOLDER_HASH = '2a96cbd8b46e442fc41c2b86b821562f';

export function useArtistDiscovery({
  artistName,
  enabled,
}: {
  artistName: string;
  enabled: boolean;
}) {
  const isFetchEnabled = useDetailFetchEnabled();
  const port = useDetailPort();

  const { data, isLoading, isError, error, refetch } = useQuery({
    ...resolveEntityQuery('artist', artistName, 1, port),
    enabled: enabled && isFetchEnabled,
  });
  const searchResult = data?.[0] ?? null;
  const retrySearch = useGatedRefetch(refetch);

  useLoggedSearchFailure(error, { kind: 'artist', title: artistName, artist: null });

  const rawImageUrl = searchResult?.image_url ?? null;
  const isPlaceholder = rawImageUrl != null && rawImageUrl.includes(LASTFM_PLACEHOLDER_HASH);
  const imageUrl = isPlaceholder ? null : rawImageUrl;

  return {
    imageUrl,
    sources: searchResult?.sources ?? [],
    isLoading,
    isError,
    refetch: retrySearch,
  };
}
