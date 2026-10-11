import type {
  DiscoveryResult,
  DiscoverySearchHistoryResponse,
  DiscoverySearchResponse,
  DiscoverySuggestResponse,
} from '@shared/apiClient/discovery';

import type { DiscoverPort } from '@features/discovery/ports/discoverPort';

const SUGGESTION_KIND = 'query';
const SUGGESTION_POPULARITY = 0;
const SEEDED_EXECUTED_AT = '2026-01-01T00:00:00Z';

export interface MemoryDiscoverSeed {
  results?: DiscoveryResult[];
  suggestions?: string[];
  history?: string[];
}

interface PageWindow {
  limit?: number | undefined;
  offset?: number | undefined;
}

function abortError(): Error {
  return Object.assign(new Error('The operation was aborted'), { name: 'AbortError' });
}

function rejectIfAborted(signal: AbortSignal | undefined): void {
  if (signal?.aborted === true) throw abortError();
}

async function resolveOnLaterTick<T>(
  signal: AbortSignal | undefined,
  produce: () => T,
): Promise<T> {
  rejectIfAborted(signal);
  await Promise.resolve();
  rejectIfAborted(signal);
  return produce();
}

function windowOf<T>(items: readonly T[], { limit, offset = 0 }: PageWindow): T[] {
  const end = limit === undefined ? items.length : offset + limit;
  return items.slice(offset, end);
}

function searchPage(
  discovery: readonly DiscoveryResult[],
  params: { q: string; limit?: number | undefined; offset?: number | undefined },
): DiscoverySearchResponse {
  const offset = params.offset ?? 0;
  const results = windowOf(discovery, { limit: params.limit, offset });
  return {
    query: params.q,
    query_norm: params.q.trim().toLowerCase(),
    results,
    sections: [],
    providers: [],
    partial: false,
    cache: { hit: false, fetched_at: null },
    total: discovery.length,
    offset,
    has_more: offset + results.length < discovery.length,
  };
}

function suggestionsFor(
  suggestions: readonly string[],
  params: { q: string; limit?: number | undefined },
): DiscoverySuggestResponse {
  const prefix = params.q.trim().toLowerCase();
  const matching = suggestions.filter((text) => text.toLowerCase().startsWith(prefix));
  return {
    suggestions: windowOf(matching, { limit: params.limit }).map((text) => ({
      text,
      kind: SUGGESTION_KIND,
      popularity: SUGGESTION_POPULARITY,
    })),
  };
}

function historyPage(
  history: readonly string[],
  params: { limit?: number | undefined } = {},
): DiscoverySearchHistoryResponse {
  return {
    items: windowOf(history, { limit: params.limit }).map((query) => ({
      query,
      query_norm: query.trim().toLowerCase(),
      executed_at: SEEDED_EXECUTED_AT,
    })),
    total: history.length,
  };
}

export function createMemoryDiscoverPort(seed: MemoryDiscoverSeed = {}): DiscoverPort {
  const results = [...(seed.results ?? [])];
  const suggestions = [...(seed.suggestions ?? [])];
  const history = [...(seed.history ?? [])];

  return {
    searchDiscovery: (params, signal) =>
      resolveOnLaterTick(signal, () => searchPage(results, params)),
    suggestDiscovery: (params, signal) =>
      resolveOnLaterTick(signal, () => suggestionsFor(suggestions, params)),
    listSearchHistory: (params) =>
      resolveOnLaterTick(undefined, () => historyPage(history, params)),
    recordSearchHistory: (query) =>
      resolveOnLaterTick(undefined, () => {
        history.unshift(query);
      }),
  };
}
