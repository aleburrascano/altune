import { useQueryClient } from '@tanstack/react-query';

import type { TrackResponse } from '@shared/api-client/types';

import { findTrackInLibraryCache } from '../helpers/find-track-in-library-cache';

export function useLibraryTrackMatch(title: string, artist: string | null): TrackResponse | null {
  const queryClient = useQueryClient();
  return findTrackInLibraryCache(queryClient, title, artist);
}
