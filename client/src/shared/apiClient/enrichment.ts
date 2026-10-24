import { ContractError } from '@shared/failure/errors';
import { PROVIDER_STATUSES, parseDiscoveryResult } from '@shared/apiClient/discovery';
import { discoveryEntityPath, sourceIdList } from '@shared/apiClient/ids';
import { apiFetch, LONG_REQUEST_TIMEOUT_MS, signalInit, withQuery } from '@shared/apiClient/index';
import {
  asArray,
  parseArray,
  asBoolean,
  asHttpsUrl,
  asNumber,
  asRecord,
  asString,
  member,
  nullableBoolean,
  nullableNumber,
  nullableString,
} from '@shared/apiClient/wireDecoders';

import type {
  DiscoveryKind,
  DiscoveryProviderStatus,
  DiscoveryResult,
  DiscoverySource,
} from '@shared/apiClient/discovery';

export type ContentFetchResponse = {
  items: DiscoveryResult[];
  provider_name: string;
  status: DiscoveryProviderStatus;
};

function parseStringArray(value: unknown, at: string): string[] {
  return parseArray(value, at, asString);
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
    items: parseArray(r.items, `${at}.items`, parseDiscoveryResult),
    provider_name: asString(r.provider_name, `${at}.provider_name`),
    status: member(r.status, PROVIDER_STATUSES, `${at}.status`),
  };
}

export type AlbumTracksRequest = {
  provider: string;
  externalId: string;
  limit?: number | undefined;
  albumTitle?: string | undefined;
  albumArtist?: string | undefined;
  mbExternalId?: string | undefined;
  knownIds?: readonly DiscoverySource[] | undefined;
  signal?: AbortSignal | undefined;
};

export async function getAlbumTracks({
  provider,
  externalId,
  limit,
  albumTitle,
  albumArtist,
  mbExternalId,
  knownIds,
  signal,
}: AlbumTracksRequest): Promise<ContentFetchResponse> {
  const params = new URLSearchParams();
  if (limit !== undefined) params.set('limit', String(limit));
  if (albumTitle) params.set('title', albumTitle);
  if (albumArtist) params.set('artist', albumArtist);
  if (mbExternalId) params.set('mbid', mbExternalId);
  if (knownIds?.length) params.set('ids', sourceIdList(knownIds));
  const path = discoveryEntityPath('albums', provider, externalId, 'tracks');
  return parseContentFetchResponse(await apiFetch(withQuery(path, params), signalInit(signal)));
}

export async function getRelatedTracks(
  provider: string,
  externalId: string,
  limit?: number,
  signal?: AbortSignal,
): Promise<ContentFetchResponse> {
  const params = new URLSearchParams();
  if (limit !== undefined) params.set('limit', String(limit));
  const path = discoveryEntityPath('tracks', provider, externalId, 'related');
  return parseContentFetchResponse(await apiFetch(withQuery(path, params), signalInit(signal)));
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
  return parseEnrichmentResponse(
    await apiFetch(withQuery('/v1/discovery/enrichment', qs), signalInit(params.signal)),
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
    await apiFetch(
      withQuery(
        '/v1/discovery/enrichment/lastfm',
        kindTitleQs(params.kind, params.title, params.subtitle),
      ),
      signalInit(params.signal),
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
    await apiFetch(
      withQuery(
        '/v1/discovery/enrichment/deezer',
        kindTitleQs(params.kind, params.title, params.subtitle),
      ),
      signalInit(params.signal),
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

export type ArtistContentOptions = {
  tracksLimit?: number;
  albumsLimit?: number;
  knownIds?: readonly DiscoverySource[];
};

export async function getArtistContent(
  provider: string,
  externalId: string,
  opts: ArtistContentOptions = {},
  signal?: AbortSignal,
): Promise<ArtistContentResponse> {
  const params = new URLSearchParams();
  if (opts.tracksLimit !== undefined) params.set('tracks_limit', String(opts.tracksLimit));
  if (opts.albumsLimit !== undefined) params.set('albums_limit', String(opts.albumsLimit));
  if (opts.knownIds?.length) params.set('ids', sourceIdList(opts.knownIds));
  const path = discoveryEntityPath('artists', provider, externalId, 'content');
  return parseArtistContentResponse(await apiFetch(withQuery(path, params), signalInit(signal)));
}

const AUDIO_PROVIDER = /^[a-z][a-z0-9]{1,31}$/;

export type UnreleasedAudioLink = { provider: string; external_id: string; url: string };

export type UnreleasedTrack = {
  key: string;
  genius_id: number | null;
  tracker_key: string | null;
  title: string;
  quality: string | null;
  cover_url: string | null;
  genius_url: string | null;
  release_date: string | null;
  has_media: boolean | null;
  audio_links: UnreleasedAudioLink[];
  extras: Record<string, unknown>;
};

export type UnreleasedTracksResponse = { items: UnreleasedTrack[]; pending: boolean };

function nullableHttpsUrl(value: unknown, at: string): string | null {
  return value == null ? null : asHttpsUrl(value, at);
}

function unreleasedKey(geniusId: number | null, trackerKey: string | null, at: string): string {
  const key = trackerKey ?? (geniusId === null ? null : String(geniusId));
  if (key === null) throw new ContractError(at, 'expected a genius_id or a tracker_key');
  return key;
}

function parseUnreleasedIdentity(r: Record<string, unknown>, at: string) {
  const genius_id = nullableNumber(r.genius_id, `${at}.genius_id`);
  const tracker_key = nullableString(r.tracker_key, `${at}.tracker_key`);
  const key = unreleasedKey(genius_id, tracker_key, at);
  const genius_url = nullableHttpsUrl(r.genius_url, `${at}.genius_url`);
  return { key, genius_id, tracker_key, title: asString(r.title, `${at}.title`), genius_url };
}

function isAudioProvider(value: unknown): value is string {
  return typeof value === 'string' && AUDIO_PROVIDER.test(value);
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === 'string' && value !== '';
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

function isAudioLink(value: unknown): value is UnreleasedAudioLink {
  if (!isRecord(value) || !isAudioProvider(value.provider)) return false;
  return isNonEmptyString(value.external_id) && isNonEmptyString(value.url);
}

function toAudioLink({ provider, external_id, url }: UnreleasedAudioLink): UnreleasedAudioLink {
  return { provider, external_id, url };
}

function parseAudioLinks(value: unknown): UnreleasedAudioLink[] {
  const links: UnreleasedAudioLink[] = [];
  if (!Array.isArray(value)) return links;
  for (const item of value) {
    if (isAudioLink(item)) links.push(toAudioLink(item));
  }
  return links;
}

const EXTRAS_FROM_A_SERVER_WITHOUT_OWNERSHIP: Readonly<Record<string, unknown>> = Object.freeze({});

function parseOwnershipExtras(value: unknown, at: string): Record<string, unknown> {
  return value === undefined ? EXTRAS_FROM_A_SERVER_WITHOUT_OWNERSHIP : asRecord(value, at);
}

function parseUnreleasedDetails(r: Record<string, unknown>, at: string) {
  return {
    quality: nullableString(r.quality, `${at}.quality`),
    cover_url: nullableString(r.cover_url, `${at}.cover_url`),
    release_date: nullableString(r.release_date, `${at}.release_date`),
    has_media: nullableBoolean(r.has_media, `${at}.has_media`),
    audio_links: parseAudioLinks(r.audio_links),
    extras: parseOwnershipExtras(r.extras, `${at}.extras`),
  };
}

function parseUnreleasedTrack(value: unknown, at: string): UnreleasedTrack {
  const r = asRecord(value, at);
  return { ...parseUnreleasedIdentity(r, at), ...parseUnreleasedDetails(r, at) };
}

export type UnreleasedTracksRequest = {
  name: string;
  knownIds?: readonly DiscoverySource[];
};

function unreleasedParams({ name, knownIds }: UnreleasedTracksRequest): URLSearchParams {
  const params = new URLSearchParams({ name });
  if (knownIds?.length) params.set('ids', sourceIdList(knownIds));
  return params;
}

function parseUnreleasedRow(value: unknown, at: string): UnreleasedTrack | null {
  try {
    return parseUnreleasedTrack(value, at);
  } catch {
    return null;
  }
}

function isParsed(track: UnreleasedTrack | null): track is UnreleasedTrack {
  return track !== null;
}

function parseUnreleasedTracksResponse(value: unknown): UnreleasedTracksResponse {
  const at = 'UnreleasedTracksResponse';
  const body = asRecord(value, at);
  const rows = parseArray(body.items, `${at}.items`, parseUnreleasedRow);
  return {
    items: rows.filter(isParsed),
    pending: nullableBoolean(body.pending, `${at}.pending`) === true,
  };
}

async function fetchUnreleasedTracks(
  url: string,
  signal: AbortSignal | undefined,
): Promise<UnreleasedTracksResponse> {
  const init = { ...signalInit(signal), timeoutMs: LONG_REQUEST_TIMEOUT_MS };
  return parseUnreleasedTracksResponse(await apiFetch(url, init));
}

export async function getUnreleasedTracks(
  provider: string,
  externalId: string,
  request: UnreleasedTracksRequest,
  signal?: AbortSignal,
): Promise<UnreleasedTracksResponse> {
  const path = discoveryEntityPath('artists', provider, externalId, 'unreleased');
  return fetchUnreleasedTracks(withQuery(path, unreleasedParams(request)), signal);
}
