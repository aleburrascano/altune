import { apiFetch } from './index';

import type { DiscoveryKind, DiscoveryProviderStatus, DiscoveryResult } from './discovery';

export type ContentFetchResponse = {
  items: DiscoveryResult[];
  provider: string;
  status: DiscoveryProviderStatus;
  latency_ms: number;
};

function _contentUrl(basePath: string, limit?: number): string {
  if (limit === undefined) return basePath;
  return `${basePath}?limit=${limit}`;
}

export async function getAlbumTracks(
  provider: string,
  externalId: string,
  limit?: number,
  albumTitle?: string,
  albumArtist?: string,
  mbExternalId?: string,
): Promise<ContentFetchResponse> {
  const params = new URLSearchParams();
  if (limit !== undefined) params.set('limit', String(limit));
  if (albumTitle) params.set('title', albumTitle);
  if (albumArtist) params.set('artist', albumArtist);
  if (mbExternalId) params.set('mbid', mbExternalId);
  const qs = params.toString();
  const url = `/v1/discovery/albums/${provider}/${encodeURIComponent(externalId)}/tracks${qs ? `?${qs}` : ''}`;
  return apiFetch<ContentFetchResponse>(url);
}

export async function getArtistTopTracks(
  provider: string,
  externalId: string,
  limit?: number,
  artistName?: string,
): Promise<ContentFetchResponse> {
  const params = new URLSearchParams();
  if (limit !== undefined) params.set('limit', String(limit));
  if (artistName) params.set('name', artistName);
  const qs = params.toString();
  const url = `/v1/discovery/artists/${provider}/${encodeURIComponent(externalId)}/top-tracks${qs ? `?${qs}` : ''}`;
  return apiFetch<ContentFetchResponse>(url);
}

export async function getArtistAlbums(
  provider: string,
  externalId: string,
  limit?: number,
  artistName?: string,
): Promise<ContentFetchResponse> {
  const params = new URLSearchParams();
  if (limit !== undefined) params.set('limit', String(limit));
  if (artistName) params.set('name', artistName);
  const qs = params.toString();
  const url = `/v1/discovery/artists/${provider}/${encodeURIComponent(externalId)}/albums${qs ? `?${qs}` : ''}`;
  return apiFetch<ContentFetchResponse>(url);
}

export async function getRelatedTracks(
  provider: string,
  externalId: string,
  limit?: number,
): Promise<ContentFetchResponse> {
  return apiFetch<ContentFetchResponse>(
    _contentUrl(
      `/v1/discovery/tracks/${provider}/${encodeURIComponent(externalId)}/related`,
      limit,
    ),
  );
}

export type EnrichmentResponse = {
  has_content: boolean;
  mbid: string;
  genres: string[];
  year: number;
  rating: number;
  rating_votes: number;
  primary_type: string;
  secondary_types: string[];
  external_ids: Record<string, string>;
  artwork_url: string;
};

export async function getEnrichment(params: {
  kind: DiscoveryKind;
  title?: string | undefined;
  subtitle?: string | null | undefined;
  mbid?: string | undefined;
}): Promise<EnrichmentResponse> {
  const qs = new URLSearchParams({ kind: params.kind });
  if (params.title) qs.set('title', params.title);
  if (params.subtitle) qs.set('subtitle', params.subtitle);
  if (params.mbid) qs.set('mbid', params.mbid);
  return apiFetch<EnrichmentResponse>(`/v1/discovery/enrichment?${qs.toString()}`);
}

export type LastFmEnrichmentResponse = {
  has_content: boolean;
  mbid: string;
  listeners: number;
  playcount: number;
  tags: string[];
  bio: string;
  similar: string[];
  duration: number;
  album: string;
};

function kindTitleQs(kind: DiscoveryKind, title: string, subtitle?: string | null): string {
  const qs = new URLSearchParams({ kind, title });
  if (subtitle) qs.set('subtitle', subtitle);
  return qs.toString();
}

export async function getLastFmEnrichment(params: {
  kind: DiscoveryKind;
  title: string;
  subtitle?: string | null | undefined;
}): Promise<LastFmEnrichmentResponse> {
  return apiFetch<LastFmEnrichmentResponse>(
    `/v1/discovery/enrichment/lastfm?${kindTitleQs(params.kind, params.title, params.subtitle)}`,
  );
}

export type DeezerEnrichmentResponse = {
  has_content: boolean;
  bpm: number;
  gain: number;
  explicit: boolean;
  label: string;
  genres: string[];
  upc: string;
  record_type: string;
  featured_artists?: unknown[];
};

export async function getDeezerEnrichment(params: {
  kind: DiscoveryKind;
  title: string;
  subtitle?: string | null | undefined;
}): Promise<DeezerEnrichmentResponse> {
  return apiFetch<DeezerEnrichmentResponse>(
    `/v1/discovery/enrichment/deezer?${kindTitleQs(params.kind, params.title, params.subtitle)}`,
  );
}

export type ArtistContentResponse = {
  top_tracks: ContentFetchResponse;
  albums: ContentFetchResponse;
};

export async function getArtistContent(
  provider: string,
  externalId: string,
  opts: { artistName?: string; tracksLimit?: number; albumsLimit?: number } = {},
): Promise<ArtistContentResponse> {
  const params = new URLSearchParams();
  if (opts.artistName) params.set('name', opts.artistName);
  if (opts.tracksLimit !== undefined) params.set('tracks_limit', String(opts.tracksLimit));
  if (opts.albumsLimit !== undefined) params.set('albums_limit', String(opts.albumsLimit));
  const qs = params.toString();
  const url = `/v1/discovery/artists/${provider}/${encodeURIComponent(externalId)}/content${qs ? `?${qs}` : ''}`;
  return apiFetch<ArtistContentResponse>(url);
}
