import type { DiscoveryResult, DiscoverySearchResponse } from '@shared/apiClient/discovery';
import type { AlbumGroup, ArtistGroup } from '@shared/apiClient/library';
import type {
  FeaturedArtist,
  ListTracksResponse,
  TrackResponse,
} from '@shared/apiClient/trackResponse';
import type { PlaylistDetailResponse, PlaylistResponse } from '@shared/apiClient/playlistResponse';
import { MAX_ALL_TRACKS } from '@shared/apiClient/tracks';
import { ApiError } from '@shared/failure/errors';

import type { LibraryPort } from '@features/library/ports/libraryPort';

const NOT_FOUND_STATUS = 404;

export interface MemoryLibrarySeed {
  tracks?: TrackResponse[];
  albums?: AlbumGroup[];
  artists?: ArtistGroup[];
  playlists?: PlaylistDetailResponse[];
  discovery?: DiscoveryResult[];
}

interface PageWindow {
  limit?: number | undefined;
  offset?: number | undefined;
}

function abortError(): Error {
  return Object.assign(new Error('The operation was aborted'), { name: 'AbortError' });
}

function rejectIfAborted(signal: AbortSignal | undefined): void {
  if (signal?.aborted === true) throw abortError();
}

async function resolveOnLaterTick<T>(
  signal: AbortSignal | undefined,
  produce: () => T,
): Promise<T> {
  rejectIfAborted(signal);
  await Promise.resolve();
  rejectIfAborted(signal);
  return produce();
}

function windowOf<T>(items: readonly T[], { limit, offset = 0 }: PageWindow): T[] {
  const end = limit === undefined ? items.length : offset + limit;
  return items.slice(offset, end);
}

interface TrackWindow {
  limit: number;
  offset: number;
}

function trackPage(items: readonly TrackResponse[], window: TrackWindow) {
  const served = windowOf(items, window);
  const response: ListTracksResponse = {
    items: served,
    total: items.length,
    limit: window.limit,
    offset: window.offset,
    has_more: window.offset + served.length < items.length,
  };
  return response;
}

function isSameArtist(artist: FeaturedArtist, wanted: FeaturedArtist): boolean {
  const sameMbid = wanted.mbid !== null && artist.mbid === wanted.mbid;
  const sameDeezerId = wanted.deezer_id !== null && artist.deezer_id === wanted.deezer_id;
  return sameMbid || sameDeezerId || artist.name === wanted.name;
}

function features(track: TrackResponse, wanted: FeaturedArtist): boolean {
  const featured = track.featured_artists ?? [];
  return featured.some((artist) => isSameArtist(artist, wanted));
}

function summaryOf({
  total_duration_seconds: _duration,
  tracks: _tracks,
  ...summary
}: PlaylistDetailResponse): PlaylistResponse {
  return summary;
}

function notFound(what: string): ApiError {
  return new ApiError(NOT_FOUND_STATUS, `${what} not found`);
}

function discoveryPage(
  discovery: readonly DiscoveryResult[],
  params: { q: string; limit?: number | undefined; offset?: number | undefined },
): DiscoverySearchResponse {
  const offset = params.offset ?? 0;
  const results = windowOf(discovery, { limit: params.limit, offset });
  return {
    query: params.q,
    query_norm: params.q.trim().toLowerCase(),
    results,
    sections: [],
    providers: [],
    partial: false,
    cache: { hit: false, fetched_at: null },
    total: discovery.length,
    offset,
    has_more: offset + results.length < discovery.length,
  };
}

export function createMemoryLibraryPort(seed: MemoryLibrarySeed = {}): LibraryPort {
  let tracks = [...(seed.tracks ?? [])];
  const albums = [...(seed.albums ?? [])];
  const artists = [...(seed.artists ?? [])];
  const playlists = [...(seed.playlists ?? [])];
  const discovery = [...(seed.discovery ?? [])];

  return {
    getTracks: ({ limit, offset }, signal) =>
      resolveOnLaterTick(signal, () => trackPage(tracks, { limit, offset })),
    getAllTracks: ({ signal }) =>
      resolveOnLaterTick(signal, () => ({
        items: tracks.slice(0, MAX_ALL_TRACKS),
        truncated: tracks.length > MAX_ALL_TRACKS,
      })),
    listTracksFeaturing: async (wanted) => {
      const featuring = tracks.filter((track) => features(track, wanted));
      return trackPage(featuring, { limit: featuring.length, offset: 0 });
    },
    deleteTrack: async (trackId) => {
      if (!tracks.some((track) => track.id === trackId)) throw notFound(`track ${trackId}`);
      tracks = tracks.filter((track) => track.id !== trackId);
    },
    reacquireTrack: async (trackId) => {
      if (!tracks.some((track) => track.id === trackId)) throw notFound(`track ${trackId}`);
    },
    getLibraryAlbums: (query = {}, signal) =>
      resolveOnLaterTick(signal, () => ({ items: windowOf(albums, query), total: albums.length })),
    getLibraryArtists: (query = {}, signal) =>
      resolveOnLaterTick(signal, () => ({
        items: windowOf(artists, query),
        total: artists.length,
      })),
    getPlaylists: (page = {}, signal) =>
      resolveOnLaterTick(signal, () => ({
        items: windowOf(playlists, page).map(summaryOf),
        total: playlists.length,
      })),
    getPlaylist: async (id) => {
      const found = playlists.find((playlist) => playlist.id === id);
      if (found === undefined) throw notFound(`playlist ${id}`);
      return found;
    },
    searchDiscovery: (params, signal) =>
      resolveOnLaterTick(signal, () => discoveryPage(discovery, params)),
  };
}
