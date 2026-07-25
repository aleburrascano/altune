import { apiFetch } from './index';

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
  const response = await apiFetch<DiscoverySearchResponse>(
    `/v1/discovery/search?${qs.toString()}`,
    signal ? { signal } : undefined,
  );
  return {
    ...response,
    results: (response.results ?? []).map(normalizeResult),
    ...(response.top_result ? { top_result: normalizeResult(response.top_result) } : {}),
    sections: (response.sections ?? []).map((section) => ({
      ...section,
      items: section.items.map(normalizeResult),
    })),
    total: response.total ?? (response.results ?? []).length,
    offset: response.offset ?? 0,
    has_more: response.has_more ?? false,
  };
}

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
