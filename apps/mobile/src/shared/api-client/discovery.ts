/**
 * Typed client for the discovery search surface: search, suggest, history,
 * plus the core wire types. The behavioral /events send (`recordEvent`) lives in
 * `@shared/telemetry` — it is telemetry, not a search call, and keeping it here
 * made api-client import up into telemetry (structure audit F1).
 *
 * Slice 43 of discover-music-v1. Mirrors the wire shape in
 * docs/specs/discover-music-v1/spec.md §3.7. The detail-open surface
 * (catalog browse + per-provider enrichment) lives in enrichment.ts.
 */

import { apiFetch } from './index';

export type DiscoveryKind = 'artist' | 'album' | 'track';
export type DiscoveryConfidence = 'high' | 'medium' | 'low';
export type DiscoveryProviderStatus =
  | 'ok'
  | 'timeout'
  | 'error'
  | 'rate_limited'
  | 'circuit_open';

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
  // Server-computed stable identity — (kind, normalized title, normalized
  // subtitle). The cross-query join key the client echoes on every engagement
  // event. Present only on results that came from the discovery wire; absent on
  // results synthesized client-side (library → discovery conversions).
  result_signature?: string | undefined;
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

export type DiscoverySearchResponse = {
  query: string;
  query_norm: string;
  // The search_id keystone minted per search. Echoed back on every engagement
  // event (results_shown, result_clicked, …) so the funnel joins to its search.
  search_id?: string | undefined;
  results: DiscoveryResult[];
  providers: DiscoveryProviderInfo[];
  partial: boolean;
  cache: { hit: boolean; fetched_at: string | null };
  corrected_query?: string;
  original_query?: string;
  related?: RelatedGroup[];
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

export async function searchDiscovery(
  params: {
    q: string;
    kinds?: DiscoveryKind[];
    limit?: number;
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
  if (params.saveHistory === false) {
    qs.set('save_history', 'false');
  }
  const response = await apiFetch<DiscoverySearchResponse>(
    `/v1/discovery/search?${qs.toString()}`,
    signal ? { signal } : undefined,
  );
  return { ...response, results: (response.results ?? []).map(normalizeResult) };
}

// The wire omits an empty `subtitle`/`image_url` (Go `omitempty`), so an absent
// value arrives as `undefined` despite the declared `string | null` type. Coerce
// to null at the boundary so every `!== null` guard downstream behaves as the
// type promises — otherwise a result with the artist baked into its title and no
// separate subtitle crashes the detail screen on `undefined.length`.
function normalizeResult(r: DiscoveryResult): DiscoveryResult {
  return { ...r, subtitle: r.subtitle ?? null, image_url: r.image_url ?? null };
}

export async function suggestDiscovery(params: {
  q: string;
  limit?: number;
}): Promise<DiscoverySuggestResponse> {
  const qs = new URLSearchParams({ q: params.q });
  if (params.limit !== undefined) {
    qs.set('limit', String(params.limit));
  }
  return apiFetch<DiscoverySuggestResponse>(`/v1/discovery/suggest?${qs.toString()}`);
}

export async function listSearchHistory(params?: {
  limit?: number;
}): Promise<DiscoverySearchHistoryResponse> {
  const qs = new URLSearchParams();
  if (params?.limit !== undefined) {
    qs.set('limit', String(params.limit));
  }
  const query = qs.toString();
  return apiFetch<DiscoverySearchHistoryResponse>(
    `/v1/discovery/search-history${query ? `?${query}` : ''}`,
  );
}

export async function clearSearchHistory(): Promise<void> {
  await apiFetch<void>('/v1/discovery/search-history', { method: 'DELETE' });
}

