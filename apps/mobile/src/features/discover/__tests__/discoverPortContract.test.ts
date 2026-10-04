import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { supabase } from '@shared/auth/supabaseClient';

import { httpDiscoverPort } from '@features/discover/adapters/http/httpDiscoverPort';
import { createMemoryDiscoverPort } from '@features/discover/__tests__/memoryDiscoverPort';
import { resultFixture } from '@features/discover/__tests__/fixtures';
import { useAutocompleteSuggestions } from '@features/discover/hooks/useAutocompleteSuggestions';
import { DiscoverPortProvider } from '@features/discover/ports/DiscoverPortProvider';
import type { DiscoverPort } from '@features/discover/ports/discoverPort';

const { __http, fakeFetch } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const SEARCH_PATH = '/v1/discovery/search';
const SUGGEST_PATH = '/v1/discovery/suggest';
const HISTORY_PATH = '/v1/discovery/search-history';
const SUGGESTION_KIND = 'query';
const EXECUTED_AT = '2026-01-01T00:00:00Z';
const SEEDED_COUNT = 2;
const HISTORY_LIMIT = 10;

interface PortSeed {
  results: DiscoveryResult[];
  suggestions: string[];
  history: string[];
}

function resultsTitled(count: number): DiscoveryResult[] {
  return Array.from({ length: count }, (_unused, index) =>
    resultFixture({ title: `title ${index}`, result_signature: `sig-${index}` }),
  );
}

function seedOf(overrides: Partial<PortSeed> = {}): PortSeed {
  return { results: [], suggestions: [], history: [], ...overrides };
}

function serveDiscoverOverHttp(seed: PortSeed): DiscoverPort {
  __http.reply(`GET ${SEARCH_PATH}`, {
    json: {
      query: 'q',
      query_norm: 'q',
      results: seed.results,
      sections: [],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: seed.results.length,
      offset: 0,
      has_more: false,
    },
  });
  __http.reply(`GET ${SUGGEST_PATH}`, {
    json: {
      suggestions: seed.suggestions.map((text) => ({ text, kind: SUGGESTION_KIND, popularity: 0 })),
    },
  });
  __http.reply(`GET ${HISTORY_PATH}`, {
    json: {
      items: seed.history.map((query) => ({
        query,
        query_norm: query,
        executed_at: EXECUTED_AT,
      })),
      total: seed.history.length,
    },
  });
  return httpDiscoverPort;
}

function abortedSignal(): AbortSignal {
  const controller = new AbortController();
  controller.abort();
  return controller.signal;
}

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
});

afterEach(() => {
  __http.reset();
  global.fetch = fakeFetch;
});

describe.each<[string, (seed: PortSeed) => DiscoverPort]>([
  ['createMemoryDiscoverPort', (seed) => createMemoryDiscoverPort(seed)],
  ['httpDiscoverPort over HTTP', serveDiscoverOverHttp],
])('%s satisfies the DiscoverPort contract', (_name, seededPort) => {
  it('searches and serves the seeded results', async () => {
    const port = seededPort(seedOf({ results: resultsTitled(SEEDED_COUNT) }));

    const response = await port.searchDiscovery({ q: 'q' });

    expect(response.results.map((found) => found.title)).toEqual(['title 0', 'title 1']);
    expect(response.total).toBe(SEEDED_COUNT);
  });

  it('suggests the seeded suggestions', async () => {
    const port = seededPort(seedOf({ suggestions: ['radiohead', 'radio'] }));

    const response = await port.suggestDiscovery({ q: 'radio' });

    expect(response.suggestions.map((suggestion) => suggestion.text)).toEqual([
      'radiohead',
      'radio',
    ]);
  });

  it('lists the seeded search history', async () => {
    const port = seededPort(seedOf({ history: ['first', 'second'] }));

    const response = await port.listSearchHistory({ limit: HISTORY_LIMIT });

    expect(response.items.map((item) => item.query)).toEqual(['first', 'second']);
    expect(response.total).toBe(SEEDED_COUNT);
  });

  it('serves empty results, suggestions and history from an empty seed', async () => {
    const port = seededPort(seedOf());

    const search = await port.searchDiscovery({ q: 'q' });
    const suggest = await port.suggestDiscovery({ q: 'q' });
    const history = await port.listSearchHistory();

    expect(search.results).toEqual([]);
    expect(search.has_more).toBe(false);
    expect(suggest.suggestions).toEqual([]);
    expect(history).toEqual({ items: [], total: 0 });
  });

  it.each<[string, (port: DiscoverPort, signal: AbortSignal) => Promise<unknown>]>([
    ['searchDiscovery', (port, signal) => port.searchDiscovery({ q: 'q' }, signal)],
    ['suggestDiscovery', (port, signal) => port.suggestDiscovery({ q: 'q' }, signal)],
  ])('rejects %s with an AbortError when the signal is already aborted', async (_m, call) => {
    const port = seededPort(seedOf());

    await expect(call(port, abortedSignal())).rejects.toMatchObject({ name: 'AbortError' });
  });

  it.each<[string, (port: DiscoverPort, signal: AbortSignal) => Promise<unknown>]>([
    ['searchDiscovery', (port, signal) => port.searchDiscovery({ q: 'q' }, signal)],
    ['suggestDiscovery', (port, signal) => port.suggestDiscovery({ q: 'q' }, signal)],
  ])(
    'rejects %s with an AbortError when the signal aborts right after the call starts',
    async (_m, call) => {
      const port = seededPort(seedOf());
      const controller = new AbortController();

      const calling = call(port, controller.signal);
      controller.abort();

      await expect(calling).rejects.toMatchObject({ name: 'AbortError' });
    },
  );
});

describe('useAutocompleteSuggestions on the memory fake', () => {
  it('returns the seeded suggestions with no api-client mock', async () => {
    const queryClient = new QueryClient();
    const port = createMemoryDiscoverPort({ suggestions: ['radiohead'] });
    function wrapper({ children }: { children: React.ReactNode }) {
      return React.createElement(
        QueryClientProvider,
        { client: queryClient },
        DiscoverPortProvider({ port, children }),
      );
    }

    const { result } = renderHook(() => useAutocompleteSuggestions('radio'), { wrapper });

    await waitFor(() => expect(result.current.data?.suggestions).toHaveLength(1));
    expect(result.current.data?.suggestions[0]?.text).toBe('radiohead');
  });
});
