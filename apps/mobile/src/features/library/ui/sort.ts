import type { TrackResponse } from '@shared/api-client/types';

import type { AlbumGroup, ArtistGroup } from '../hooks/useLibraryGrouping';

export type SortKey = 'recent' | 'az' | 'year';

export function sortAlbums(albums: AlbumGroup[], key: SortKey): AlbumGroup[] {
  const sorted = [...albums];
  switch (key) {
    case 'recent':
      return sorted.sort((a, b) => b.mostRecentAddedAt.localeCompare(a.mostRecentAddedAt));
    case 'az':
      return sorted.sort((a, b) => a.album.localeCompare(b.album));
    case 'year':
      return sorted.sort((a, b) => (b.year ?? 0) - (a.year ?? 0));
  }
}

export function sortArtists(artists: ArtistGroup[], key: SortKey): ArtistGroup[] {
  const sorted = [...artists];
  switch (key) {
    case 'recent':
      return sorted.sort((a, b) => b.mostRecentAddedAt.localeCompare(a.mostRecentAddedAt));
    case 'az':
      return sorted.sort((a, b) => a.artist.localeCompare(b.artist));
    case 'year':
      return sorted;
  }
}

export function sortTracks(tracks: TrackResponse[], key: SortKey): TrackResponse[] {
  const sorted = [...tracks];
  switch (key) {
    case 'recent':
      return sorted.sort((a, b) => b.added_at.localeCompare(a.added_at));
    case 'az':
      return sorted.sort((a, b) => a.title.localeCompare(b.title));
    case 'year':
      return sorted.sort((a, b) => (b.year ?? 0) - (a.year ?? 0));
  }
}

export const ALBUM_SORT_OPTIONS: { key: SortKey; label: string }[] = [
  { key: 'recent', label: 'Recent' },
  { key: 'az', label: 'A–Z' },
  { key: 'year', label: 'Year' },
];

export const ARTIST_SORT_OPTIONS: { key: SortKey; label: string }[] = [
  { key: 'recent', label: 'Recent' },
  { key: 'az', label: 'A–Z' },
];

export const TRACK_SORT_OPTIONS: { key: SortKey; label: string }[] = [
  { key: 'recent', label: 'Recent' },
  { key: 'az', label: 'A–Z' },
];
