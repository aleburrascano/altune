import { useMemo } from 'react';
import { skipToken, useQuery } from '@tanstack/react-query';

import type { ListTracksResponse, TrackResponse } from '@shared/api-client/types';
import { libraryKeys } from '@shared/lib/query-keys';

import { findTrackInData } from '../helpers/find-track-in-library-cache';

export function useLibraryTrackMatch(title: string, artist: string | null): TrackResponse | null {
  const { data: home } = useQuery<ListTracksResponse>({
    queryKey: libraryKeys.home,
    queryFn: skipToken,
  });
  return useMemo(() => findTrackInData(home, title, artist), [home, title, artist]);
}
