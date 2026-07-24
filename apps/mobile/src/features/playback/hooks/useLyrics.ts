import { useQuery } from '@tanstack/react-query';

import { getLyrics } from '@shared/api-client/lyrics';
import { discoveryKeys } from '@shared/lib/query-keys';

const LYRICS_STALE_MS = 24 * 60 * 60 * 1000;

export function useLyrics(track: { title: string; artist: string } | null) {
  const title = track?.title ?? '';
  const artist = track?.artist ?? '';
  return useQuery({
    queryKey: discoveryKeys.lyrics(title, artist),
    queryFn: () => getLyrics({ title, subtitle: artist }),
    enabled: title.length > 0,
    staleTime: LYRICS_STALE_MS,
  });
}
