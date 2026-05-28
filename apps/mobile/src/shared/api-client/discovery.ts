/**
 * Typed client for the discovery endpoints.
 *
 * Slice 43 of discover-music-v1. Mirrors the wire shape in
 * docs/specs/discover-music-v1/spec.md §3.7.
 */

import { apiFetch } from './index';

export type DiscoveryKind = 'artist' | 'album' | 'track' | 'playlist';
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
  sources: DiscoverySource[];
  extras: Record<string, unknown>;
};

export type DiscoveryProviderInfo = {
  provider: string;
  status: DiscoveryProviderStatus;
  result_count: number;
  latency_ms: number;
};

export type DiscoverySearchResponse = {
  query: string;
  query_norm: string;
  results: DiscoveryResult[];
  providers: DiscoveryProviderInfo[];
  partial: boolean;
  cache: { hit: boolean; fetched_at: string | null };
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

export type ClickPayload = {
  query_norm: string;
  kind: DiscoveryKind;
  title: string;
  subtitle?: string | null;
  position: number;
  confidence: DiscoveryConfidence;
};

export async function searchDiscovery(params: {
  q: string;
  kinds?: DiscoveryKind[];
  limit?: number;
}): Promise<DiscoverySearchResponse> {
  const qs = new URLSearchParams({ q: params.q });
  if (params.kinds && params.kinds.length > 0) {
    qs.set('kinds', params.kinds.join(','));
  }
  if (params.limit !== undefined) {
    qs.set('limit', String(params.limit));
  }
  return apiFetch<DiscoverySearchResponse>(`/v1/discovery/search?${qs.toString()}`);
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

export async function recordClick(payload: ClickPayload): Promise<void> {
  await apiFetch<void>('/v1/discovery/clicks', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
}
