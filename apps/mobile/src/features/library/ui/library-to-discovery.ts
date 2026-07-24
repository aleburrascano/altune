import type { DiscoveryResult } from '@shared/api-client/discovery';

import type { AlbumGroup, ArtistGroup } from '../hooks/useLibraryGrouping';

export function albumToDiscoveryResult(album: AlbumGroup): DiscoveryResult {
  return {
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
}

export function artistToDiscoveryResult(artist: ArtistGroup): DiscoveryResult {
  return {
    kind: 'artist',
    title: artist.artist,
    subtitle: null,
    image_url: artist.artworkUrl,
    confidence: 'high',
    sources: [],
    extras: {},
  };
}
