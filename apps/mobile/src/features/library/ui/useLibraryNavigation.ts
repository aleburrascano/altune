import { useCallback } from 'react';
import type { useRouter } from 'expo-router';

import { setDetailHandoff } from '@shared/lib/detail-handoff';
import { trackToDiscoveryResult } from '@shared/lib/track-to-discovery';
import type { TrackResponse } from '@shared/api-client/types';

import type { AlbumGroup, ArtistGroup } from '../hooks/useLibraryGrouping';
import { albumToDiscoveryResult, artistToDiscoveryResult } from './library-to-discovery';

export function useLibraryNavigation(router: ReturnType<typeof useRouter>) {
  const navigateToTrack = useCallback(
    (track: TrackResponse): void => {
      setDetailHandoff(trackToDiscoveryResult(track));
      router.push('/library/detail');
    },
    [router],
  );

  const navigateToAlbum = useCallback(
    (album: AlbumGroup): void => {
      setDetailHandoff(albumToDiscoveryResult(album));
      router.push('/library/detail');
    },
    [router],
  );

  const navigateToArtist = useCallback(
    (artist: ArtistGroup): void => {
      setDetailHandoff(artistToDiscoveryResult(artist));
      router.push('/library/detail');
    },
    [router],
  );

  return { navigateToTrack, navigateToAlbum, navigateToArtist };
}
