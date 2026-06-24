import type { DiscoveryResult } from '@shared/api-client/discovery';

import type { AlbumGroup, ArtistGroup } from '../hooks/useLibraryGrouping';

/**
 * Adapts library client-side groupings into the discovery wire shape, so an
 * album or artist tapped in the library flows through the same detail-handoff
 * path as a discovery result.
 *
 * Siblings of `trackToDiscoveryResult` (shared/lib, 2+ consumers). Album/artist
 * stay feature-local because library is their only consumer — the mapping
 * invariant (`confidence: 'high'`, empty `sources`, conditional `extras`) lives
 * in one tested place instead of as inline literals in the navigation hook.
 * Promote to shared/ on the second consumer (YAGNI).
 */
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
