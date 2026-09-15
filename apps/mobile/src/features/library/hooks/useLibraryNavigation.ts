import { useCallback } from 'react';
import type { useRouter } from 'expo-router';

import { detailHref } from '@shared/lib/detail-handoff';
import { trackToDiscoveryResult } from '@shared/lib/track-to-discovery';
import type { TrackResponse } from '@shared/api-client/types';

import type { AlbumGroup, ArtistGroup } from '@shared/api-client/library';
import { albumToDiscoveryResult, artistToDiscoveryResult } from '../library-to-discovery';

export function useLibraryNavigation(router: ReturnType<typeof useRouter>) {
  const navigateToTrack = useCallback(
    (track: TrackResponse): void => {
      router.push(detailHref('/library/detail', trackToDiscoveryResult(track)));
    },
    [router],
  );

  const navigateToAlbum = useCallback(
    (album: AlbumGroup): void => {
      router.push(detailHref('/library/detail', albumToDiscoveryResult(album)));
    },
    [router],
  );

  const navigateToArtist = useCallback(
    (artist: ArtistGroup): void => {
      router.push(detailHref('/library/detail', artistToDiscoveryResult(artist)));
    },
    [router],
  );

  return { navigateToTrack, navigateToAlbum, navigateToArtist };
}
