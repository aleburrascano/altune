import { apiFetch, signalInit } from './index';
import { withQuery } from './queryString';
import {
  asArray,
  asNumber,
  asRecord,
  asString,
  nullableNumber,
  nullableString,
} from './wireDecoders';

export type LibrarySort = 'recent' | 'az' | 'year';

export type LibraryQuery = {
  q?: string;
  sort?: LibrarySort;
  /** Rows to ask for. Omitted, the server picks its own page size. */
  limit?: number;
  /** Rows to skip before the page starts. Omitted, the server starts at the first row. */
  offset?: number;
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

function parseAlbumGroup(value: unknown, at: string): AlbumGroup {
  const r = asRecord(value, at);
  return {
    key: asString(r.key, `${at}.key`),
    album: asString(r.album, `${at}.album`),
    artist: asString(r.artist, `${at}.artist`),
    artwork_url: nullableString(r.artwork_url, `${at}.artwork_url`),
    year: nullableNumber(r.year, `${at}.year`),
    track_count: asNumber(r.track_count, `${at}.track_count`),
    most_recent_added_at: asString(r.most_recent_added_at, `${at}.most_recent_added_at`),
  };
}

function parseArtistGroup(value: unknown, at: string): ArtistGroup {
  const r = asRecord(value, at);
  return {
    key: asString(r.key, `${at}.key`),
    artist: asString(r.artist, `${at}.artist`),
    artwork_url: nullableString(r.artwork_url, `${at}.artwork_url`),
    track_count: asNumber(r.track_count, `${at}.track_count`),
    most_recent_added_at: asString(r.most_recent_added_at, `${at}.most_recent_added_at`),
  };
}

export function parseListAlbumsResponse(
  value: unknown,
  at = 'ListAlbumsResponse',
): ListAlbumsResponse {
  const r = asRecord(value, at);
  return {
    items: asArray(r.items, `${at}.items`).map((item, i) =>
      parseAlbumGroup(item, `${at}.items[${i}]`),
    ),
    total: asNumber(r.total, `${at}.total`),
  };
}

export function parseListArtistsResponse(
  value: unknown,
  at = 'ListArtistsResponse',
): ListArtistsResponse {
  const r = asRecord(value, at);
  return {
    items: asArray(r.items, `${at}.items`).map((item, i) =>
      parseArtistGroup(item, `${at}.items[${i}]`),
    ),
    total: asNumber(r.total, `${at}.total`),
  };
}

function libraryParams(query: LibraryQuery): URLSearchParams {
  const params = new URLSearchParams();
  if (query.q) params.set('q', query.q);
  if (query.sort) params.set('sort', query.sort);
  if (query.limit !== undefined) params.set('limit', String(query.limit));
  if (query.offset !== undefined) params.set('offset', String(query.offset));
  return params;
}

export async function getLibraryAlbums(
  query: LibraryQuery = {},
  signal?: AbortSignal,
): Promise<ListAlbumsResponse> {
  return parseListAlbumsResponse(
    await apiFetch<unknown>(
      withQuery('/v1/library/albums', libraryParams(query)),
      signalInit(signal),
    ),
  );
}

export async function getLibraryArtists(
  query: LibraryQuery = {},
  signal?: AbortSignal,
): Promise<ListArtistsResponse> {
  return parseListArtistsResponse(
    await apiFetch<unknown>(
      withQuery('/v1/library/artists', libraryParams(query)),
      signalInit(signal),
    ),
  );
}
