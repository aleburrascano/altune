import type { TrackResponse } from '@shared/api-client/types';

export type AlbumGroup = {
  key: string;
  album: string;
  artist: string;
  artworkUrl: string | null;
  year: number | null;
  trackCount: number;
  mostRecentAddedAt: string;
};

export type ArtistGroup = {
  key: string;
  artist: string;
  artworkUrl: string | null;
  trackCount: number;
  mostRecentAddedAt: string;
};

function groupKey(album: string | null, artist: string | null): string {
  return `${(album ?? '').toLowerCase()}|||${(artist ?? '').toLowerCase()}`;
}

// Shared group fields every grouping mutates on a repeat-hit (track count,
// most-recent timestamp, first-available artwork). Albums and artists differ
// only in their skip/key/create — the fold below is identical.
type GroupBase = {
  artworkUrl: string | null;
  trackCount: number;
  mostRecentAddedAt: string;
};

function deriveGroups<T extends GroupBase>(
  tracks: TrackResponse[],
  opts: {
    skip?: (t: TrackResponse) => boolean;
    keyOf: (t: TrackResponse) => string;
    create: (t: TrackResponse, key: string) => T;
  },
): T[] {
  const map = new Map<string, T>();
  for (const t of tracks) {
    if (opts.skip?.(t) ?? false) continue;
    const k = opts.keyOf(t);
    const existing = map.get(k);
    if (existing == null) {
      map.set(k, opts.create(t, k));
    } else {
      existing.trackCount += 1;
      if (t.added_at > existing.mostRecentAddedAt) {
        existing.mostRecentAddedAt = t.added_at;
      }
      if (existing.artworkUrl == null && t.artwork_url != null) {
        existing.artworkUrl = t.artwork_url;
      }
    }
  }
  return [...map.values()];
}

export function deriveAlbums(tracks: TrackResponse[]): AlbumGroup[] {
  return deriveGroups<AlbumGroup>(tracks, {
    skip: (t) => t.album == null || t.album.length === 0,
    keyOf: (t) => groupKey(t.album, t.album_artist ?? t.artist),
    create: (t, key) => ({
      key,
      album: t.album as string,
      artist: t.album_artist ?? t.artist,
      artworkUrl: t.artwork_url,
      year: t.year,
      trackCount: 1,
      mostRecentAddedAt: t.added_at,
    }),
  });
}

export function deriveArtists(tracks: TrackResponse[]): ArtistGroup[] {
  return deriveGroups<ArtistGroup>(tracks, {
    keyOf: (t) => t.artist.toLowerCase(),
    create: (t, key) => ({
      key,
      artist: t.artist,
      artworkUrl: t.artwork_url,
      trackCount: 1,
      mostRecentAddedAt: t.added_at,
    }),
  });
}
