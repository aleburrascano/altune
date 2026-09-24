import { PROVIDER_STATUSES, parseDiscoveryResult } from './discovery';
import { apiFetch } from './index';
import { withQuery } from './queryString';
import { asArray, asBoolean, asNumber, asRecord, asString, member } from './wireDecoders';

import type { DiscoveryKind, DiscoveryProviderStatus, DiscoveryResult } from './discovery';

/**
 * The fields ContentFetchResponseDTO (content_endpoints.go) actually sends. The
 * type previously also declared `provider` and `latency_ms`: the DTO calls the
 * first `provider_name` and has never carried the second, so both were always
 * undefined at runtime (#1777).
 */
export type ContentFetchResponse = {
  items: DiscoveryResult[];
  provider_name: string;
  status: DiscoveryProviderStatus;
};

function parseStringArray(value: unknown, at: string): string[] {
  return asArray(value, at).map((item, i) => asString(item, `${at}[${i}]`));
}

function parseStringMap(value: unknown, at: string): Record<string, string> {
  const record = asRecord(value, at);
  return Object.fromEntries(
    Object.entries(record).map(([key, item]) => [key, asString(item, `${at}.${key}`)]),
  );
}

function parseContentFetchResponse(
  value: unknown,
  at = 'ContentFetchResponse',
): ContentFetchResponse {
  const r = asRecord(value, at);
  return {
    items: asArray(r.items, `${at}.items`).map((item, i) =>
      parseDiscoveryResult(item, `${at}.items[${i}]`),
    ),
    provider_name: asString(r.provider_name, `${at}.provider_name`),
    // A 200 can still carry a degraded half (artist content), and the caller
    // shows results only for 'ok', so an unrecognized status must not read as one.
    status: member(r.status, PROVIDER_STATUSES, `${at}.status`),
  };
}

export async function getAlbumTracks({
  provider,
  externalId,
  limit,
  albumTitle,
  albumArtist,
  mbExternalId,
  signal,
}: {
  provider: string;
  externalId: string;
  limit?: number | undefined;
  albumTitle?: string | undefined;
  albumArtist?: string | undefined;
  mbExternalId?: string | undefined;
  signal?: AbortSignal | undefined;
}): Promise<ContentFetchResponse> {
  const params = new URLSearchParams();
  if (limit !== undefined) params.set('limit', String(limit));
  if (albumTitle) params.set('title', albumTitle);
  if (albumArtist) params.set('artist', albumArtist);
  if (mbExternalId) params.set('mbid', mbExternalId);
  const path = `/v1/discovery/albums/${encodeURIComponent(provider)}/${encodeURIComponent(externalId)}/tracks`;
  return parseContentFetchResponse(
    await apiFetch<unknown>(withQuery(path, params), signal ? { signal } : undefined),
  );
}

export async function getArtistTopTracks({
  provider,
  externalId,
  limit,
  artistName,
}: {
  provider: string;
  externalId: string;
  limit?: number | undefined;
  artistName?: string | undefined;
}): Promise<ContentFetchResponse> {
  const params = new URLSearchParams();
  if (limit !== undefined) params.set('limit', String(limit));
  if (artistName) params.set('name', artistName);
  const path = `/v1/discovery/artists/${encodeURIComponent(provider)}/${encodeURIComponent(externalId)}/top-tracks`;
  return parseContentFetchResponse(await apiFetch<unknown>(withQuery(path, params)));
}

export async function getArtistAlbums({
  provider,
  externalId,
  limit,
  artistName,
}: {
  provider: string;
  externalId: string;
  limit?: number | undefined;
  artistName?: string | undefined;
}): Promise<ContentFetchResponse> {
  const params = new URLSearchParams();
  if (limit !== undefined) params.set('limit', String(limit));
  if (artistName) params.set('name', artistName);
  const path = `/v1/discovery/artists/${encodeURIComponent(provider)}/${encodeURIComponent(externalId)}/albums`;
  return parseContentFetchResponse(await apiFetch<unknown>(withQuery(path, params)));
}

export async function getRelatedTracks(
  provider: string,
  externalId: string,
  limit?: number,
  signal?: AbortSignal,
): Promise<ContentFetchResponse> {
  const params = new URLSearchParams();
  if (limit !== undefined) params.set('limit', String(limit));
  const path = `/v1/discovery/tracks/${encodeURIComponent(provider)}/${encodeURIComponent(externalId)}/related`;
  return parseContentFetchResponse(
    await apiFetch<unknown>(withQuery(path, params), signal ? { signal } : undefined),
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

// An entity with nothing to show still answers 200 with a fully-populated empty
// payload (has_content:false), so every field is required on the wire whether or
// not the lookup found anything.
function parseEnrichmentResponse(value: unknown, at = 'EnrichmentResponse'): EnrichmentResponse {
  const r = asRecord(value, at);
  return {
    has_content: asBoolean(r.has_content, `${at}.has_content`),
    mbid: asString(r.mbid, `${at}.mbid`),
    genres: parseStringArray(r.genres, `${at}.genres`),
    year: asNumber(r.year, `${at}.year`),
    rating: asNumber(r.rating, `${at}.rating`),
    rating_votes: asNumber(r.rating_votes, `${at}.rating_votes`),
    primary_type: asString(r.primary_type, `${at}.primary_type`),
    secondary_types: parseStringArray(r.secondary_types, `${at}.secondary_types`),
    external_ids: parseStringMap(r.external_ids, `${at}.external_ids`),
    artwork_url: asString(r.artwork_url, `${at}.artwork_url`),
  };
}

export async function getEnrichment(params: {
  kind: DiscoveryKind;
  title?: string | undefined;
  subtitle?: string | null | undefined;
  mbid?: string | undefined;
  signal?: AbortSignal | undefined;
}): Promise<EnrichmentResponse> {
  const qs = new URLSearchParams({ kind: params.kind });
  if (params.title) qs.set('title', params.title);
  if (params.subtitle) qs.set('subtitle', params.subtitle);
  if (params.mbid) qs.set('mbid', params.mbid);
  return parseEnrichmentResponse(await apiFetch<unknown>(
      withQuery('/v1/discovery/enrichment', qs),
      params.signal ? { signal: params.signal } : undefined,
    ),
  );
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

function kindTitleQs(
  kind: DiscoveryKind,
  title: string,
  subtitle?: string | null,
): URLSearchParams {
  const qs = new URLSearchParams({ kind, title });
  if (subtitle) qs.set('subtitle', subtitle);
  return qs;
}

function parseLastFmEnrichmentResponse(
  value: unknown,
  at = 'LastFmEnrichmentResponse',
): LastFmEnrichmentResponse {
  const r = asRecord(value, at);
  return {
    has_content: asBoolean(r.has_content, `${at}.has_content`),
    mbid: asString(r.mbid, `${at}.mbid`),
    listeners: asNumber(r.listeners, `${at}.listeners`),
    playcount: asNumber(r.playcount, `${at}.playcount`),
    tags: parseStringArray(r.tags, `${at}.tags`),
    bio: asString(r.bio, `${at}.bio`),
    similar: parseStringArray(r.similar, `${at}.similar`),
    duration: asNumber(r.duration, `${at}.duration`),
    album: asString(r.album, `${at}.album`),
  };
}

export async function getLastFmEnrichment(params: {
  kind: DiscoveryKind;
  title: string;
  subtitle?: string | null | undefined;
  signal?: AbortSignal | undefined;
}): Promise<LastFmEnrichmentResponse> {
  return parseLastFmEnrichmentResponse(
    await apiFetch<unknown>(
      withQuery(
        '/v1/discovery/enrichment/lastfm',
        kindTitleQs(params.kind, params.title, params.subtitle),
      ),
      params.signal ? { signal: params.signal } : undefined,
    ),
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

// featured_artists is the one optional field: the DTO omits it when empty, and
// its members stay unknown here because only the detail screen names their shape.
function parseDeezerEnrichmentResponse(
  value: unknown,
  at = 'DeezerEnrichmentResponse',
): DeezerEnrichmentResponse {
  const r = asRecord(value, at);
  return {
    has_content: asBoolean(r.has_content, `${at}.has_content`),
    bpm: asNumber(r.bpm, `${at}.bpm`),
    gain: asNumber(r.gain, `${at}.gain`),
    explicit: asBoolean(r.explicit, `${at}.explicit`),
    label: asString(r.label, `${at}.label`),
    genres: parseStringArray(r.genres, `${at}.genres`),
    upc: asString(r.upc, `${at}.upc`),
    record_type: asString(r.record_type, `${at}.record_type`),
    ...(r.featured_artists != null
      ? { featured_artists: asArray(r.featured_artists, `${at}.featured_artists`) }
      : {}),
  };
}

export async function getDeezerEnrichment(params: {
  kind: DiscoveryKind;
  title: string;
  subtitle?: string | null | undefined;
  signal?: AbortSignal | undefined;
}): Promise<DeezerEnrichmentResponse> {
  return parseDeezerEnrichmentResponse(
    await apiFetch<unknown>(
      withQuery(
        '/v1/discovery/enrichment/deezer',
        kindTitleQs(params.kind, params.title, params.subtitle),
      ),
      params.signal ? { signal: params.signal } : undefined,
    ),
  );
}

export type ArtistContentResponse = {
  top_tracks: ContentFetchResponse;
  albums: ContentFetchResponse;
};

function parseArtistContentResponse(
  value: unknown,
  at = 'ArtistContentResponse',
): ArtistContentResponse {
  const r = asRecord(value, at);
  return {
    top_tracks: parseContentFetchResponse(r.top_tracks, `${at}.top_tracks`),
    albums: parseContentFetchResponse(r.albums, `${at}.albums`),
  };
}

export async function getArtistContent(
  provider: string,
  externalId: string,
  opts: { artistName?: string; tracksLimit?: number; albumsLimit?: number } = {},
  signal?: AbortSignal,
): Promise<ArtistContentResponse> {
  const params = new URLSearchParams();
  if (opts.artistName) params.set('name', opts.artistName);
  if (opts.tracksLimit !== undefined) params.set('tracks_limit', String(opts.tracksLimit));
  if (opts.albumsLimit !== undefined) params.set('albums_limit', String(opts.albumsLimit));
  const path = `/v1/discovery/artists/${encodeURIComponent(provider)}/${encodeURIComponent(externalId)}/content`;
  return parseArtistContentResponse(
    await apiFetch<unknown>(withQuery(path, params), signal ? { signal } : undefined),
  );
}
