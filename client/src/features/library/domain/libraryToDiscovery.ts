import type { AlbumGroup, ArtistGroup } from '@shared/apiClient';
import type { DiscoveryResult } from '@shared/catalog';

export function albumToDiscoveryResult(album: AlbumGroup): DiscoveryResult {
  return {
    kind: 'album',
    title: album.album,
    subtitle: album.artist,
    image_url: album.artwork_url,
    confidence: 'high',
    sources: [],
    extras: {
      ...(album.year != null ? { year: album.year } : {}),
      track_count: album.track_count,
    },
  };
}

export function artistToDiscoveryResult(artist: ArtistGroup): DiscoveryResult {
  return {
    kind: 'artist',
    title: artist.artist,
    subtitle: null,
    image_url: artist.artwork_url,
    confidence: 'high',
    sources: [],
    extras: {},
  };
}
