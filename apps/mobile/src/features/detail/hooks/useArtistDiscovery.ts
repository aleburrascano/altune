import { useQuery } from '@tanstack/react-query';

import { searchDiscovery } from '@shared/api-client/discovery';

export function useArtistDiscovery({
  artistName,
  enabled,
}: {
  artistName: string;
  enabled: boolean;
}) {
  const { data: searchResult, isLoading, isError } = useQuery({
    queryKey: ['artist-discovery-search', artistName],
    queryFn: async () => {
      const res = await searchDiscovery({
        q: artistName,
        kinds: ['artist'],
        limit: 1,
        saveHistory: false,
      });
      return res.results[0] ?? null;
    },
    enabled,
    staleTime: 30 * 60 * 1000,
  });

  const rawImageUrl = searchResult?.image_url ?? null;
  const isPlaceholder = rawImageUrl != null && rawImageUrl.includes('2a96cbd8b46e442fc41c2b86b821562f');
  const imageUrl = isPlaceholder ? null : rawImageUrl;

  return {
    artistResult: searchResult ?? null,
    imageUrl,
    sources: searchResult?.sources ?? [],
    mbid: typeof searchResult?.extras['mbid'] === 'string' ? searchResult.extras['mbid'] : null,
    isLoading,
    isError,
  };
}
