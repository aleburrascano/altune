import { asFavoriteKey, type FavoriteKey } from './ids';
import { apiFetch, signalInit } from './index';
import { withQuery } from './queryString';
import {
  parseArray,
  parseListEnvelope,
  asBoolean,
  asNumber,
  asRecord,
  asString,
  member,
  nullableString,
} from './wireDecoders';

export const DISCOVERY_KINDS = ['artist', 'album', 'track'] as const;
const DISCOVERY_CONFIDENCES = ['high', 'medium', 'low'] as const;
export const PROVIDER_STATUSES = [
  'ok',
  'timeout',
  'error',
  'rate_limited',
  'circuit_open',
] as const;

export type DiscoveryKind = (typeof DISCOVERY_KINDS)[number];
export type DiscoveryConfidence = (typeof DISCOVERY_CONFIDENCES)[number];
export type DiscoveryProviderStatus = (typeof PROVIDER_STATUSES)[number];

export type DiscoverySource = {
  provider: string;
  external_id: string;
  url: string;
};

export type DiscoveryResult = {
  kind: DiscoveryKind;
  title: string;
  subtitle: string | null;
  image_url: string | null;
  confidence: DiscoveryConfidence;
  result_signature?: string | undefined;
  favorite_key?: FavoriteKey | undefined;
  sources: DiscoverySource[];
  extras: Record<string, unknown>;
};

export type DiscoveryProviderInfo = {
  provider: string;
  status: DiscoveryProviderStatus;
  result_count: number;
  latency_ms: number;
};

export type RelatedGroup = {
  relationship: string;
  related_to: string;
  items: DiscoveryResult[];
};

export type ResultSection = {
  kind: DiscoveryKind;
  items: DiscoveryResult[];
  has_more: boolean;
};

export type DiscoverySearchResponse = {
  query: string;
  query_norm: string;
  search_id?: string | undefined;
  results: DiscoveryResult[];
  top_result?: DiscoveryResult | undefined;
  sections: ResultSection[];
  providers: DiscoveryProviderInfo[];
  partial: boolean;
  cache: { hit: boolean; fetched_at: string | null };
  corrected_query?: string;
  original_query?: string;
  related?: RelatedGroup[];
  total: number;
  offset: number;
  has_more: boolean;
};

export type DiscoverySuggestion = {
  text: string;
  kind: string;
  popularity: number;
};

export type DiscoverySuggestResponse = {
  suggestions: DiscoverySuggestion[];
};

export type SearchHistoryItem = {
  query: string;
  query_norm: string;
  executed_at: string;
};

export type DiscoverySearchHistoryResponse = {
  items: SearchHistoryItem[];
  total: number;
};

function parseDiscoverySource(value: unknown, at: string): DiscoverySource {
  const r = asRecord(value, at);
  return {
    provider: asString(r.provider, `${at}.provider`),
    external_id: asString(r.external_id, `${at}.external_id`),
    url: asString(r.url, `${at}.url`),
  };
}

function isMember<T extends string>(text: string, allowed: readonly T[]): text is T {
  return (allowed as readonly string[]).includes(text);
}

export function parseDiscoveryResult(value: unknown, at: string): DiscoveryResult {
  const r = asRecord(value, at);
  return {
    kind: member(r.kind, DISCOVERY_KINDS, `${at}.kind`),
    title: asString(r.title, `${at}.title`),
    subtitle: nullableString(r.subtitle, `${at}.subtitle`),
    image_url: nullableString(r.image_url, `${at}.image_url`),
    confidence: member(r.confidence, DISCOVERY_CONFIDENCES, `${at}.confidence`),
    sources: parseArray(r.sources, `${at}.sources`, parseDiscoverySource),
    extras: asRecord(r.extras, `${at}.extras`),
    ...(r.result_signature != null
      ? { result_signature: asString(r.result_signature, `${at}.result_signature`) }
      : {}),
    ...(r.favorite_key != null
      ? { favorite_key: asFavoriteKey(asString(r.favorite_key, `${at}.favorite_key`)) }
      : {}),
  };
}

function parseSearchResult(value: unknown, at: string): DiscoveryResult | null {
  const r = asRecord(value, at);
  if (!isMember(asString(r.kind, `${at}.kind`), DISCOVERY_KINDS)) return null;
  const confidence = asString(r.confidence, `${at}.confidence`);
  const known = isMember(confidence, DISCOVERY_CONFIDENCES) ? confidence : 'low';
  return parseDiscoveryResult({ ...r, confidence: known }, at);
}

function parseResultArray(value: unknown, at: string): DiscoveryResult[] {
  return parseArray(value, at, parseSearchResult).filter((item) => item !== null);
}

function parseProvider(value: unknown, at: string): DiscoveryProviderInfo {
  const r = asRecord(value, at);
  const status = asString(r.status, `${at}.status`);
  return {
    provider: asString(r.provider, `${at}.provider`),
    status: isMember(status, PROVIDER_STATUSES) ? status : 'error',
    result_count: asNumber(r.result_count, `${at}.result_count`),
    latency_ms: asNumber(r.latency_ms, `${at}.latency_ms`),
  };
}

function parseSection(value: unknown, at: string): ResultSection | null {
  const r = asRecord(value, at);
  const kind = asString(r.kind, `${at}.kind`);
  if (!isMember(kind, DISCOVERY_KINDS)) return null;
  return {
    kind,
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

function topResult(parsed: DiscoveryResult | null): { top_result?: DiscoveryResult } {
  return parsed === null ? {} : { top_result: parsed };
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
        : parseArray(r.sections, `${at}.sections`, parseSection).filter((s) => s !== null),
    providers: parseArray(r.providers, `${at}.providers`, parseProvider),
    partial: asBoolean(r.partial, `${at}.partial`),
    cache: parseCache(r.cache, `${at}.cache`),
    total: r.total == null ? results.length : asNumber(r.total, `${at}.total`),
    offset: r.offset == null ? 0 : asNumber(r.offset, `${at}.offset`),
    has_more: r.has_more == null ? false : asBoolean(r.has_more, `${at}.has_more`),
    ...(r.search_id != null ? { search_id: asString(r.search_id, `${at}.search_id`) } : {}),
    ...(r.top_result != null ? topResult(parseSearchResult(r.top_result, `${at}.top_result`)) : {}),
    ...(r.corrected_query != null
      ? { corrected_query: asString(r.corrected_query, `${at}.corrected_query`) }
      : {}),
    ...(r.original_query != null
      ? { original_query: asString(r.original_query, `${at}.original_query`) }
      : {}),
    ...(r.related != null
      ? {
          related: parseArray(r.related, `${at}.related`, parseRelated),
        }
      : {}),
  };
}

function parseSuggestion(value: unknown, at: string): DiscoverySuggestion {
  const r = asRecord(value, at);
  return {
    text: asString(r.text, `${at}.text`),
    kind: asString(r.kind, `${at}.kind`),
    popularity: asNumber(r.popularity, `${at}.popularity`),
  };
}

function parseDiscoverySuggestResponse(value: unknown): DiscoverySuggestResponse {
  const at = 'DiscoverySuggestResponse';
  const r = asRecord(value, at);
  return {
    suggestions: parseArray(r.suggestions, `${at}.suggestions`, parseSuggestion),
  };
}

function parseSearchHistoryItem(value: unknown, at: string): SearchHistoryItem {
  const r = asRecord(value, at);
  return {
    query: asString(r.query, `${at}.query`),
    query_norm: asString(r.query_norm, `${at}.query_norm`),
    executed_at: asString(r.executed_at, `${at}.executed_at`),
  };
}

function parseDiscoverySearchHistoryResponse(value: unknown): DiscoverySearchHistoryResponse {
  const at = 'DiscoverySearchHistoryResponse';
  const r = asRecord(value, at);
  return parseListEnvelope(r, at, parseSearchHistoryItem);
}

export async function searchDiscovery(
  params: {
    q: string;
    kinds?: DiscoveryKind[];
    limit?: number;
    offset?: number;
    saveHistory?: boolean;
    searchId?: string;
  },
  signal?: AbortSignal,
): Promise<DiscoverySearchResponse> {
  const qs = new URLSearchParams({ q: params.q });
  if (params.kinds && params.kinds.length > 0) {
    qs.set('kinds', params.kinds.join(','));
  }
  if (params.limit !== undefined) {
    qs.set('limit', String(params.limit));
  }
  if (params.offset !== undefined && params.offset > 0) {
    qs.set('offset', String(params.offset));
  }
  if (params.saveHistory === false) {
    qs.set('save_history', 'false');
  }
  if (params.searchId !== undefined) {
    qs.set('search_id', params.searchId);
  }
  const body = await apiFetch<unknown>(withQuery('/v1/discovery/search', qs), signalInit(signal));
  return parseDiscoverySearchResponse(body);
}

export async function suggestDiscovery(
  params: {
    q: string;
    limit?: number;
  },
  signal?: AbortSignal,
): Promise<DiscoverySuggestResponse> {
  const qs = new URLSearchParams({ q: params.q });
  if (params.limit !== undefined) {
    qs.set('limit', String(params.limit));
  }
  const body = await apiFetch<unknown>(withQuery('/v1/discovery/suggest', qs), signalInit(signal));
  return parseDiscoverySuggestResponse(body);
}

export async function listSearchHistory(params?: {
  limit?: number;
}): Promise<DiscoverySearchHistoryResponse> {
  const qs = new URLSearchParams();
  if (params?.limit !== undefined) {
    qs.set('limit', String(params.limit));
  }
  const body = await apiFetch<unknown>(withQuery('/v1/discovery/search-history', qs));
  return parseDiscoverySearchHistoryResponse(body);
}

export async function clearSearchHistory(): Promise<void> {
  await apiFetch<void>('/v1/discovery/search-history', { method: 'DELETE' });
}
