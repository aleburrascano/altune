import { asFavoriteKey, type FavoriteKey } from './ids';
import { apiFetch } from './index';
import { withQuery } from './queryString';
import {
  asArray,
  asBoolean,
  asNumber,
  asRecord,
  asString,
  member,
  nullableString,
} from './wireDecoders';

// Exported for the sibling endpoints that answer with the same discovery
// vocabulary (favorites' kind, enrichment's items), so one list stays the source
// of truth for what a kind may be.
export const DISCOVERY_KINDS = ['artist', 'album', 'track'] as const;
const DISCOVERY_CONFIDENCES = ['high', 'medium', 'low'] as const;
export const PROVIDER_STATUSES = [
  'ok',
  'timeout',
  'error',
  'rate_limited',
  'circuit_open',
] as const;

export type DiscoveryKind = 'artist' | 'album' | 'track';
export type DiscoveryConfidence = 'high' | 'medium' | 'low';
export type DiscoveryProviderStatus = 'ok' | 'timeout' | 'error' | 'rate_limited' | 'circuit_open';

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

export function parseDiscoveryResult(value: unknown, at: string): DiscoveryResult {
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
      ? { favorite_key: asFavoriteKey(asString(r.favorite_key, `${at}.favorite_key`)) }
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
    suggestions: asArray(r.suggestions, `${at}.suggestions`).map((item, i) =>
      parseSuggestion(item, `${at}.suggestions[${i}]`),
    ),
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
  return {
    items: asArray(r.items, `${at}.items`).map((item, i) =>
      parseSearchHistoryItem(item, `${at}.items[${i}]`),
    ),
    total: asNumber(r.total, `${at}.total`),
  };
}

export async function searchDiscovery(
  params: {
    q: string;
    kinds?: DiscoveryKind[];
    limit?: number;
    offset?: number;
    saveHistory?: boolean;
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
  const body = await apiFetch<unknown>(
    withQuery('/v1/discovery/search', qs),
    signal ? { signal } : undefined,
  );
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
  const body = await apiFetch<unknown>(
    withQuery('/v1/discovery/suggest', qs),
    signal ? { signal } : undefined,
  );
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
