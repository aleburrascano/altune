import { ContractError } from './errors';
import type {
  AcquisitionStatus,
  ApiErrorBody,
  FeaturedArtist,
  ListTracksResponse,
  TrackResponse,
} from './types';
import type { AlbumGroup, ArtistGroup, ListAlbumsResponse, ListArtistsResponse } from './library';
import type {
  DiscoveryProviderInfo,
  DiscoveryResult,
  DiscoverySearchResponse,
  DiscoverySource,
  RelatedGroup,
  ResultSection,
} from './discovery';

const ACQUISITION_STATUSES = ['pending', 'ready', 'failed'] as const;
const DISCOVERY_KINDS = ['artist', 'album', 'track'] as const;
const DISCOVERY_CONFIDENCES = ['high', 'medium', 'low'] as const;
const PROVIDER_STATUSES = ['ok', 'timeout', 'error', 'rate_limited', 'circuit_open'] as const;

export function asRecord(value: unknown, at: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new ContractError(at, 'expected an object');
  }
  return value as Record<string, unknown>;
}

export function asArray(value: unknown, at: string): unknown[] {
  if (!Array.isArray(value)) throw new ContractError(at, 'expected an array');
  return value;
}

export function asString(value: unknown, at: string): string {
  if (typeof value !== 'string') throw new ContractError(at, 'expected a string');
  return value;
}

export function asNumber(value: unknown, at: string): number {
  if (typeof value !== 'number') throw new ContractError(at, 'expected a number');
  return value;
}

export function asBoolean(value: unknown, at: string): boolean {
  if (typeof value !== 'boolean') throw new ContractError(at, 'expected a boolean');
  return value;
}

export function nullableString(value: unknown, at: string): string | null {
  return value == null ? null : asString(value, at);
}

export function nullableNumber(value: unknown, at: string): number | null {
  return value == null ? null : asNumber(value, at);
}

function optionalString(record: Record<string, unknown>, key: string): string | undefined {
  const value = record[key];
  return typeof value === 'string' ? value : undefined;
}

export function parseErrorBody(value: unknown): ApiErrorBody {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return {};
  const record = value as Record<string, unknown>;
  const code = optionalString(record, 'code');
  const detail = optionalString(record, 'detail');
  return {
    ...(code !== undefined ? { code } : {}),
    ...(detail !== undefined ? { detail } : {}),
  };
}

export function member<T extends string>(value: unknown, allowed: readonly T[], at: string): T {
  const text = asString(value, at);
  if (!allowed.includes(text as T)) {
    throw new ContractError(at, `not one of ${allowed.join(', ')}`);
  }
  return text as T;
}

function parseFeaturedArtist(value: unknown, at: string): FeaturedArtist {
  const r = asRecord(value, at);
  return {
    name: asString(r.name, `${at}.name`),
    mbid: nullableString(r.mbid, `${at}.mbid`),
    deezer_id: nullableNumber(r.deezer_id, `${at}.deezer_id`),
  };
}

export function parseTrackResponse(value: unknown, at = 'TrackResponse'): TrackResponse {
  const r = asRecord(value, at);
  const status: AcquisitionStatus = member(
    r.acquisition_status,
    ACQUISITION_STATUSES,
    `${at}.acquisition_status`,
  );
  return {
    id: asString(r.id, `${at}.id`),
    title: asString(r.title, `${at}.title`),
    artist: asString(r.artist, `${at}.artist`),
    album: nullableString(r.album, `${at}.album`),
    duration_seconds: nullableNumber(r.duration_seconds, `${at}.duration_seconds`),
    added_at: asString(r.added_at, `${at}.added_at`),
    acquisition_status: status,
    artwork_url: nullableString(r.artwork_url, `${at}.artwork_url`),
    failure_reason: nullableString(r.failure_reason, `${at}.failure_reason`),
    year: nullableNumber(r.year, `${at}.year`),
    genre: nullableString(r.genre, `${at}.genre`),
    track_number: nullableNumber(r.track_number, `${at}.track_number`),
    album_artist: nullableString(r.album_artist, `${at}.album_artist`),
    isrc: nullableString(r.isrc, `${at}.isrc`),
    audio_ref: nullableString(r.audio_ref, `${at}.audio_ref`),
    ...(r.failure_message !== undefined
      ? { failure_message: nullableString(r.failure_message, `${at}.failure_message`) }
      : {}),
    ...(r.featured_artists !== undefined
      ? {
          featured_artists: asArray(r.featured_artists, `${at}.featured_artists`).map((item, i) =>
            parseFeaturedArtist(item, `${at}.featured_artists[${i}]`),
          ),
        }
      : {}),
  };
}

export function parseListTracksResponse(
  value: unknown,
  at = 'ListTracksResponse',
): ListTracksResponse {
  const r = asRecord(value, at);
  return {
    items: asArray(r.items, `${at}.items`).map((item, i) =>
      parseTrackResponse(item, `${at}.items[${i}]`),
    ),
    total: asNumber(r.total, `${at}.total`),
    limit: asNumber(r.limit, `${at}.limit`),
    offset: asNumber(r.offset, `${at}.offset`),
    has_more: asBoolean(r.has_more, `${at}.has_more`),
  };
}

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

function parseDiscoverySource(value: unknown, at: string): DiscoverySource {
  const r = asRecord(value, at);
  return {
    provider: asString(r.provider, `${at}.provider`),
    external_id: asString(r.external_id, `${at}.external_id`),
    url: asString(r.url, `${at}.url`),
  };
}

function parseDiscoveryResult(value: unknown, at: string): DiscoveryResult {
  const r = asRecord(value, at);
  return {
    kind: member(r.kind, DISCOVERY_KINDS, `${at}.kind`),
    title: asString(r.title, `${at}.title`),
    subtitle: nullableString(r.subtitle, `${at}.subtitle`),
    image_url: nullableString(r.image_url, `${at}.image_url`),
    confidence: member(r.confidence, DISCOVERY_CONFIDENCES, `${at}.confidence`),
    sources: asArray(r.sources, `${at}.sources`).map((item, i) =>
      parseDiscoverySource(item, `${at}.sources[${i}]`),
    ),
    extras: asRecord(r.extras, `${at}.extras`),
    ...(r.result_signature != null
      ? { result_signature: asString(r.result_signature, `${at}.result_signature`) }
      : {}),
    ...(r.favorite_key != null
      ? { favorite_key: asString(r.favorite_key, `${at}.favorite_key`) }
      : {}),
  };
}

function parseResultArray(value: unknown, at: string): DiscoveryResult[] {
  return asArray(value, at).map((item, i) => parseDiscoveryResult(item, `${at}[${i}]`));
}

function parseProvider(value: unknown, at: string): DiscoveryProviderInfo {
  const r = asRecord(value, at);
  return {
    provider: asString(r.provider, `${at}.provider`),
    status: member(r.status, PROVIDER_STATUSES, `${at}.status`),
    result_count: asNumber(r.result_count, `${at}.result_count`),
    latency_ms: asNumber(r.latency_ms, `${at}.latency_ms`),
  };
}

function parseSection(value: unknown, at: string): ResultSection {
  const r = asRecord(value, at);
  return {
    kind: member(r.kind, DISCOVERY_KINDS, `${at}.kind`),
    items: parseResultArray(r.items, `${at}.items`),
    has_more: asBoolean(r.has_more, `${at}.has_more`),
  };
}

function parseRelated(value: unknown, at: string): RelatedGroup {
  const r = asRecord(value, at);
  return {
    relationship: asString(r.relationship, `${at}.relationship`),
    related_to: asString(r.related_to, `${at}.related_to`),
    items: parseResultArray(r.items, `${at}.items`),
  };
}

function parseCache(value: unknown, at: string): { hit: boolean; fetched_at: string | null } {
  const r = asRecord(value, at);
  return {
    hit: asBoolean(r.hit, `${at}.hit`),
    fetched_at: nullableString(r.fetched_at, `${at}.fetched_at`),
  };
}

export function parseDiscoverySearchResponse(value: unknown): DiscoverySearchResponse {
  const at = 'DiscoverySearchResponse';
  const r = asRecord(value, at);
  const results = r.results == null ? [] : parseResultArray(r.results, `${at}.results`);
  return {
    query: asString(r.query, `${at}.query`),
    query_norm: asString(r.query_norm, `${at}.query_norm`),
    results,
    sections:
      r.sections == null
        ? []
        : asArray(r.sections, `${at}.sections`).map((item, i) =>
            parseSection(item, `${at}.sections[${i}]`),
          ),
    providers: asArray(r.providers, `${at}.providers`).map((item, i) =>
      parseProvider(item, `${at}.providers[${i}]`),
    ),
    partial: asBoolean(r.partial, `${at}.partial`),
    cache: parseCache(r.cache, `${at}.cache`),
    total: r.total == null ? results.length : asNumber(r.total, `${at}.total`),
    offset: r.offset == null ? 0 : asNumber(r.offset, `${at}.offset`),
    has_more: r.has_more == null ? false : asBoolean(r.has_more, `${at}.has_more`),
    ...(r.search_id != null ? { search_id: asString(r.search_id, `${at}.search_id`) } : {}),
    ...(r.top_result != null
      ? { top_result: parseDiscoveryResult(r.top_result, `${at}.top_result`) }
      : {}),
    ...(r.corrected_query != null
      ? { corrected_query: asString(r.corrected_query, `${at}.corrected_query`) }
      : {}),
    ...(r.original_query != null
      ? { original_query: asString(r.original_query, `${at}.original_query`) }
      : {}),
    ...(r.related != null
      ? {
          related: asArray(r.related, `${at}.related`).map((item, i) =>
            parseRelated(item, `${at}.related[${i}]`),
          ),
        }
      : {}),
  };
}
