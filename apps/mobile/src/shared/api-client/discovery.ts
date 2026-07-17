/**
 * Typed client for the discovery search surface: search, suggest, history,
 * and the behavioral /events envelope, plus the core wire types.
 *
 * Slice 43 of discover-music-v1. Mirrors the wire shape in
 * docs/specs/discover-music-v1/spec.md §3.7. The detail-open surface
 * (catalog browse + per-provider enrichment) lives in enrichment.ts.
 */

import { getSessionId } from '@shared/telemetry/session';

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

// Behavioral interaction events, all routed through the unified /events envelope
// (the legacy /clicks endpoint was folded into this — clicks are now a
// result_clicked event). query_norm is top-level so the no-click coverage signal
// can match it; everything else rides in payload.
export type DiscoveryEventType =
  | 'results_shown'
  | 'result_clicked'
  | 'play'
  | 'skip'
  | 'completed'
  | 'library_add'
  | 'wrong_album';

export type DiscoveryEvent = {
  type: DiscoveryEventType;
  query_norm?: string;
  // The originating search's keystone. Threaded onto every engagement event so
  // the backend can join the impression/click/play funnel back to its search.
  search_id?: string | undefined;
  // Two-tier reliability fields, set only for the label-critical outbox tier
  // (library_add, wrong_album): an idempotency key the server dedups on, and the
  // client's record time (vs the server received_at).
  event_id?: string | undefined;
  client_occurred_at?: string | undefined;
  payload?: Record<string, unknown>;
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

export async function recordEvent(event: DiscoveryEvent): Promise<void> {
  // Stamp the rotating session_id onto every event's payload (no column — it
  // rides in JSONB) so the backend can derive session-arc signals (abandonment,
  // pogo-sticking) without each call site threading it.
  const body: DiscoveryEvent = {
    ...event,
    payload: { ...(event.payload ?? {}), session_id: getSessionId() },
  };
  await apiFetch<void>('/v1/discovery/events', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}
