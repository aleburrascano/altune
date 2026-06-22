/**
 * useLyrics — fetch a track's Deezer lyrics on detail open.
 *
 * Resolves the plain + time-synced lyrics from the track's title + subtitle
 * (artist). A track with no lyrics (or none for this region) comes back empty and
 * is surfaced as `lyrics: null` so the section hides. Tracks only — there is no
 * album/artist lyrics surface. Off the search path — one cached call per open
 * (docs/providers/deezer.md cap 6).
 */

import { useQuery } from '@tanstack/react-query';

import { getLyrics, type LyricsResponse } from '@shared/api-client/discovery';

type UseLyricsParams = {
  title: string;
  subtitle?: string | null | undefined;
  enabled?: boolean;
};

type UseLyricsReturn = {
  lyrics: LyricsResponse | null;
  isLoading: boolean;
  isError: boolean;
};

// hasContent reports whether a payload carries lyrics worth rendering.
function hasContent(l: LyricsResponse): boolean {
  return l.plain.trim() !== '' || l.synced_lines.length > 0;
}

export function useLyrics({
  title,
  subtitle,
  enabled = true,
}: UseLyricsParams): UseLyricsReturn {
  const canFetch = enabled && title.trim() !== '';

  const { data, isLoading, isError } = useQuery({
    queryKey: ['lyrics', `${title}|${subtitle ?? ''}`],
    queryFn: () => getLyrics({ title, subtitle }),
    enabled: canFetch,
    staleTime: 1000 * 60 * 60 * 24,
  });

  return {
    lyrics: data && hasContent(data) ? data : null,
    isLoading,
    isError,
  };
}
