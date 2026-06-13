import { useMemo } from 'react';

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

function deriveAlbums(tracks: TrackResponse[]): AlbumGroup[] {
  const map = new Map<string, AlbumGroup>();
  for (const t of tracks) {
    if (t.album == null || t.album.length === 0) continue;
    const k = groupKey(t.album, t.album_artist ?? t.artist);
    const existing = map.get(k);
    if (existing == null) {
      map.set(k, {
        key: k,
        album: t.album,
        artist: t.album_artist ?? t.artist,
        artworkUrl: t.artwork_url,
        year: t.year,
        trackCount: 1,
        mostRecentAddedAt: t.added_at,
      });
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

function deriveArtists(tracks: TrackResponse[]): ArtistGroup[] {
  const map = new Map<string, ArtistGroup>();
  for (const t of tracks) {
    const k = t.artist.toLowerCase();
    const existing = map.get(k);
    if (existing == null) {
      map.set(k, {
        key: k,
        artist: t.artist,
        artworkUrl: t.artwork_url,
        trackCount: 1,
        mostRecentAddedAt: t.added_at,
      });
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

export function useLibraryGrouping(tracks: TrackResponse[]) {
  const albums = useMemo(() => deriveAlbums(tracks), [tracks]);
  const artists = useMemo(() => deriveArtists(tracks), [tracks]);
  return { albums, artists };
}
