import type { DiscoveryResult, DiscoverySearchResponse } from '@shared/api-client/discovery';
import type {
  ArtistContentResponse,
  ContentFetchResponse,
  DeezerEnrichmentResponse,
  EnrichmentResponse,
  LastFmEnrichmentResponse,
} from '@shared/api-client/enrichment';
import type { AlbumGroup } from '@shared/api-client/library';
import { asTrackId } from '@shared/api-client/ids';
import type {
  CreateTrackRequest,
  ListTracksResponse,
  TrackResponse,
} from '@shared/api-client/types';

import type { DetailPort } from '@features/detail/ports/detailPort';

export interface MemoryDetailSeed {
  tracks?: TrackResponse[];
  albums?: AlbumGroup[];
  discovery?: DiscoveryResult[];
  enrichment?: Record<string, EnrichmentResponse>;
  lastFm?: Record<string, LastFmEnrichmentResponse>;
  deezer?: Record<string, DeezerEnrichmentResponse>;
  albumTracks?: Record<string, ContentFetchResponse>;
  relatedTracksByExternalId?: Record<string, ContentFetchResponse>;
  artistContent?: Record<string, ArtistContentResponse>;
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

function emptyContent(): ContentFetchResponse {
  return { items: [], provider_name: 'memory', status: 'ok' };
}

function emptyEnrichment(): EnrichmentResponse {
  return {
    has_content: false,
    mbid: '',
    genres: [],
    year: 0,
    rating: 0,
    rating_votes: 0,
    primary_type: '',
    secondary_types: [],
    external_ids: {},
    artwork_url: '',
  };
}

function emptyLastFm(): LastFmEnrichmentResponse {
  return {
    has_content: false,
    mbid: '',
    listeners: 0,
    playcount: 0,
    tags: [],
    bio: '',
    similar: [],
    duration: 0,
    album: '',
  };
}

function emptyDeezer(): DeezerEnrichmentResponse {
  return {
    has_content: false,
    bpm: 0,
    gain: 0,
    explicit: false,
    label: '',
    genres: [],
    upc: '',
    record_type: '',
  };
}

function trackFrom(body: CreateTrackRequest, id: string): TrackResponse {
  return {
    id: asTrackId(id),
    title: body.title,
    artist: body.artist,
    album: body.album,
    duration_seconds: body.duration_seconds,
    added_at: '2024-01-01T00:00:00Z',
    acquisition_status: 'pending',
    artwork_url: body.artwork_url,
    year: body.year,
    genre: body.genre,
    track_number: body.track_number,
    album_artist: body.album_artist,
    isrc: body.isrc,
    ...(body.featured_artists === undefined ? {} : { featured_artists: body.featured_artists }),
  } as TrackResponse;
}

function trackPage(items: readonly TrackResponse[], limit: number, offset: number) {
  const served = windowOf(items, { limit, offset });
  const response: ListTracksResponse = {
    items: served,
    total: items.length,
    limit,
    offset,
    has_more: offset + served.length < items.length,
  };
  return response;
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

export function createMemoryDetailPort(seed: MemoryDetailSeed = {}): DetailPort {
  const tracks = [...(seed.tracks ?? [])];
  const albums = [...(seed.albums ?? [])];
  const discovery = [...(seed.discovery ?? [])];
  const enrichment = { ...seed.enrichment };
  const lastFm = { ...seed.lastFm };
  const deezer = { ...seed.deezer };
  const albumTracks = { ...seed.albumTracks };
  const relatedTracks = { ...seed.relatedTracksByExternalId };
  const artistContent = { ...seed.artistContent };
  let created = 0;

  return {
    searchDiscovery: (params, signal) =>
      resolveOnLaterTick(signal, () => discoveryPage(discovery, params)),
    getEnrichment: ({ title = '', signal }) =>
      resolveOnLaterTick(signal, () => enrichment[title] ?? emptyEnrichment()),
    getLastFmEnrichment: ({ title, signal }) =>
      resolveOnLaterTick(signal, () => lastFm[title] ?? emptyLastFm()),
    getDeezerEnrichment: ({ title, signal }) =>
      resolveOnLaterTick(signal, () => deezer[title] ?? emptyDeezer()),
    getAlbumTracks: ({ albumTitle = '', signal }) =>
      resolveOnLaterTick(signal, () => albumTracks[albumTitle] ?? emptyContent()),
    getRelatedTracks: (_provider, externalId, _limit, signal) =>
      resolveOnLaterTick(signal, () => relatedTracks[externalId] ?? emptyContent()),
    getArtistContent: (_provider, _externalId, { artistName = '' } = {}, signal) =>
      resolveOnLaterTick(
        signal,
        () => artistContent[artistName] ?? { top_tracks: emptyContent(), albums: emptyContent() },
      ),
    createTrack: async (body) => {
      created += 1;
      const track = trackFrom(body, `created-${created}`);
      tracks.push(track);
      return track;
    },
    getTracks: ({ limit, offset }, signal) =>
      resolveOnLaterTick(signal, () => trackPage(tracks, limit, offset)),
    getLibraryAlbums: (query = {}, signal) =>
      resolveOnLaterTick(signal, () => ({ items: windowOf(albums, query), total: albums.length })),
  };
}
