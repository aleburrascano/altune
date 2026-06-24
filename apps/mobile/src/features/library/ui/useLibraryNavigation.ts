import { useCallback } from 'react';
import { useRouter } from 'expo-router';

import { setDetailHandoff } from '@shared/lib/detail-handoff';
import { trackToDiscoveryResult } from '@shared/lib/track-to-discovery';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { TrackResponse } from '@shared/api-client/types';

import type { AlbumGroup, ArtistGroup } from '../hooks/useLibraryGrouping';

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
      const result: DiscoveryResult = {
        kind: 'album',
        title: album.album,
        subtitle: album.artist,
        image_url: album.artworkUrl,
        confidence: 'high',
        sources: [],
        extras: {
          ...(album.year != null ? { year: album.year } : {}),
          track_count: album.trackCount,
        },
      };
      setDetailHandoff(result);
      router.push('/library/detail');
    },
    [router],
  );

  const navigateToArtist = useCallback(
    (artist: ArtistGroup): void => {
      const result: DiscoveryResult = {
        kind: 'artist',
        title: artist.artist,
        subtitle: null,
        image_url: artist.artworkUrl,
        confidence: 'high',
        sources: [],
        extras: {},
      };
      setDetailHandoff(result);
      router.push('/library/detail');
    },
    [router],
  );

  return { navigateToTrack, navigateToAlbum, navigateToArtist };
}
