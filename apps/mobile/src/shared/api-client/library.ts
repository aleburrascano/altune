import { apiFetch } from './index';

export type LibrarySort = 'recent' | 'az' | 'year';

export type LibraryQuery = {
  q?: string;
  sort?: LibrarySort;
};

export type AlbumGroup = {
  key: string;
  album: string;
  artist: string;
  artwork_url: string | null;
  year: number | null;
  track_count: number;
  most_recent_added_at: string;
};

export type ArtistGroup = {
  key: string;
  artist: string;
  artwork_url: string | null;
  track_count: number;
  most_recent_added_at: string;
};

export type ListAlbumsResponse = {
  items: AlbumGroup[];
  total: number;
};

export type ListArtistsResponse = {
  items: ArtistGroup[];
  total: number;
};

function libraryQueryString(query: LibraryQuery): string {
  const qs = new URLSearchParams();
  if (query.q) qs.set('q', query.q);
  if (query.sort) qs.set('sort', query.sort);
  const encoded = qs.toString();
  return encoded ? `?${encoded}` : '';
}

export async function getLibraryAlbums(query: LibraryQuery = {}): Promise<ListAlbumsResponse> {
  return apiFetch<ListAlbumsResponse>(`/v1/library/albums${libraryQueryString(query)}`);
}

export async function getLibraryArtists(query: LibraryQuery = {}): Promise<ListArtistsResponse> {
  return apiFetch<ListArtistsResponse>(`/v1/library/artists${libraryQueryString(query)}`);
}
