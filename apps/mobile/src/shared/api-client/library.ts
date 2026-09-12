import { apiFetch } from './index';
import { parseListAlbumsResponse, parseListArtistsResponse } from './parse';
import { withQuery } from './queryString';

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

function libraryParams(query: LibraryQuery): URLSearchParams {
  const params = new URLSearchParams();
  if (query.q) params.set('q', query.q);
  if (query.sort) params.set('sort', query.sort);
  return params;
}

export async function getLibraryAlbums(query: LibraryQuery = {}): Promise<ListAlbumsResponse> {
  return parseListAlbumsResponse(
    await apiFetch<unknown>(withQuery('/v1/library/albums', libraryParams(query))),
  );
}

export async function getLibraryArtists(query: LibraryQuery = {}): Promise<ListArtistsResponse> {
  return parseListArtistsResponse(
    await apiFetch<unknown>(withQuery('/v1/library/artists', libraryParams(query))),
  );
}
