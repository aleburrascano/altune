import { useQuery } from '@tanstack/react-query';

import { playbackPort } from '@features/playback/ports/activePlaybackPort';
import { discoveryKeys } from '@shared/query/queryKeys';

const LYRICS_STALE_MS = 24 * 60 * 60 * 1000;

export function useLyrics(track: { title: string; artist: string } | null) {
  const title = track?.title ?? '';
  const artist = track?.artist ?? '';
  return useQuery({
    queryKey: discoveryKeys.lyrics(title, artist),
    queryFn: () => playbackPort().getLyrics({ title, subtitle: artist }),
    enabled: title.length > 0,
    staleTime: LYRICS_STALE_MS,
  });
}
