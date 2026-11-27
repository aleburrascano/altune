import { useQuery } from '@tanstack/react-query';

import { playbackPort } from '@features/player/ports/activePlaybackPort';
import { discoveryKeys, STALE } from '@shared/query';

export function useLyrics(track: { title: string; artist: string } | null) {
  const title = track?.title ?? '';
  const artist = track?.artist ?? '';
  return useQuery({
    queryKey: discoveryKeys.lyrics(title, artist),
    queryFn: ({ signal }) => playbackPort().getLyrics({ title, subtitle: artist }, signal),
    enabled: title.length > 0,
    staleTime: STALE.day,
  });
}
