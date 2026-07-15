import { useLibraryTrackMatch } from './useLibraryTrackMatch';

/**
 * Reactive "is this track already in my library" flag. Thin reader over the
 * reactive useLibraryTrackMatch so it re-renders on cache patches (F12).
 */
export function useIsTrackSaved(title: string, artist: string | null): boolean {
  return useLibraryTrackMatch(title, artist) !== null;
}
