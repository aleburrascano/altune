import { useQueryClient } from '@tanstack/react-query';

import { findTrackInLibraryCache } from '../helpers/find-track-in-library-cache';

export function useIsTrackSaved(title: string, artist: string | null): boolean {
  const queryClient = useQueryClient();
  return findTrackInLibraryCache(queryClient, title, artist) !== null;
}
