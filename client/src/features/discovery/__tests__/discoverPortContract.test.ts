import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import * as discoveryApi from '@shared/api-client/discovery';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import { ApiError } from '@shared/failure/errors';
import { supabase } from '@shared/auth/supabaseClient';

import { httpDiscoverPort } from '@features/discovery/adapters/http/httpDiscoverPort';
import { createMemoryDiscoverPort } from '@features/discovery/__tests__/memoryDiscoverPort';
import { resultFixture } from '@features/discovery/__tests__/fixtures';
import { useAutocompleteSuggestions } from '@features/discovery/hooks/useSuggestions';
import { useDiscoverSearch } from '@features/discovery/hooks/useDiscoverSearch';
import { useSearchHistory } from '@features/discovery/hooks/useSearchHistory';
import {
  DiscoverPortProvider,
  useDiscoverPort,
} from '@features/discovery/ports/DiscoverPortProvider';
import type { DiscoverPort } from '@features/discovery/ports/discoverPort';

const { __http, fakeFetch } = require('@testing/doubles/fetch.js');

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

const probeClients: QueryClient[] = [];

afterEach(() => {
  probeClients.splice(0).forEach((client) => client.clear());
});

function wrapperWith(port: DiscoverPort) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  probeClients.push(queryClient);
  return function wrapper({ children }: { children: React.ReactNode }) {
    return React.createElement(
      QueryClientProvider,
      { client: queryClient },
      DiscoverPortProvider({ port, children }),
    );
  };
}

describe('useDiscoverPort', () => {
  it('throws when no provider is mounted', () => {
    jest.spyOn(console, 'error').mockImplementation(() => {});

    expect(() => renderHook(() => useDiscoverPort())).toThrow('no discover port provided');
  });

  it('hands back the port the provider was given', () => {
    const port = createMemoryDiscoverPort();
    function wrapper({ children }: { children: React.ReactNode }) {
      return DiscoverPortProvider({ port, children });
    }

    const { result } = renderHook(() => useDiscoverPort(), { wrapper });

    expect(result.current).toBe(port);
  });

  it('hands back the innermost port when providers are nested', () => {
    const outer = createMemoryDiscoverPort();
    const inner = createMemoryDiscoverPort();
    function wrapper({ children }: { children: React.ReactNode }) {
      return DiscoverPortProvider({
        port: outer,
        children: DiscoverPortProvider({ port: inner, children }),
      });
    }

    const { result } = renderHook(() => useDiscoverPort(), { wrapper });

    expect(result.current).toBe(inner);
  });
});

describe('the discover hooks run on the port the provider was given', () => {
  it('useSearchHistory returns the seeded history without a network request', async () => {
    const port = createMemoryDiscoverPort({ history: ['first', 'second'] });

    const { result } = renderHook(() => useSearchHistory(), { wrapper: wrapperWith(port) });

    await waitFor(() => expect(result.current.data).toBeDefined());
    expect(result.current.data?.items.map((item) => item.query)).toEqual(['first', 'second']);
    expect(__http.requests).toHaveLength(0);
  });

  it('useDiscoverSearch returns the seeded results without a network request', async () => {
    const port = createMemoryDiscoverPort({ results: resultsTitled(SEEDED_COUNT) });

    const { result } = renderHook(() => useDiscoverSearch('radio'), {
      wrapper: wrapperWith(port),
    });

    await waitFor(() => expect(result.current.data).toBeDefined());
    expect(result.current.data?.results.map((found) => found.title)).toEqual([
      'title 0',
      'title 1',
    ]);
    expect(__http.requests).toHaveLength(0);
  });

  it('useSearchHistory surfaces the exact error the port rejected with', async () => {
    const failure = new ApiError(400, 'bad history request');
    const port: DiscoverPort = {
      ...createMemoryDiscoverPort(),
      listSearchHistory: () => Promise.reject(failure),
    };

    const { result } = renderHook(() => useSearchHistory(), { wrapper: wrapperWith(port) });

    await waitFor(() => expect(result.current.error).toBe(failure));
  });

  it('useAutocompleteSuggestions surfaces the exact error the port rejected with', async () => {
    const failure = new ApiError(400, 'bad suggest request');
    const port: DiscoverPort = {
      ...createMemoryDiscoverPort(),
      suggestDiscovery: () => Promise.reject(failure),
    };

    const { result } = renderHook(() => useAutocompleteSuggestions('radio'), {
      wrapper: wrapperWith(port),
    });

    await waitFor(() => expect(result.current.error).toBe(failure));
  });

  it('useDiscoverSearch surfaces the exact error the port rejected with', async () => {
    const failure = new ApiError(400, 'bad search request');
    const port: DiscoverPort = {
      ...createMemoryDiscoverPort(),
      searchDiscovery: () => Promise.reject(failure),
    };

    const { result } = renderHook(() => useDiscoverSearch('radio'), {
      wrapper: wrapperWith(port),
    });

    await waitFor(() => expect(result.current.error).toBe(failure), { timeout: 5000 });
  });
});

describe('httpDiscoverPort', () => {
  afterEach(() => jest.restoreAllMocks());

  it('rejects searchDiscovery with an AbortError when the signal aborts while the request is on the wire', async () => {
    __http.hang(`GET ${SEARCH_PATH}`);
    const controller = new AbortController();

    const calling = httpDiscoverPort.searchDiscovery({ q: 'q' }, controller.signal);
    await waitFor(() => expect(__http.countFor(`GET ${SEARCH_PATH}`)).toBe(1));
    controller.abort();

    await expect(calling).rejects.toMatchObject({ name: 'AbortError' });
  });

  it('rejects suggestDiscovery with an AbortError when the signal aborts while the request is on the wire', async () => {
    __http.hang(`GET ${SUGGEST_PATH}`);
    const controller = new AbortController();

    const calling = httpDiscoverPort.suggestDiscovery({ q: 'q' }, controller.signal);
    await waitFor(() => expect(__http.countFor(`GET ${SUGGEST_PATH}`)).toBe(1));
    controller.abort();

    await expect(calling).rejects.toMatchObject({ name: 'AbortError' });
  });

  it.each<[string, () => Promise<unknown>, string]>([
    ['searchDiscovery', () => httpDiscoverPort.searchDiscovery({ q: 'q' }), SEARCH_PATH],
    ['suggestDiscovery', () => httpDiscoverPort.suggestDiscovery({ q: 'q' }), SUGGEST_PATH],
    ['listSearchHistory', () => httpDiscoverPort.listSearchHistory(), HISTORY_PATH],
  ])('passes a 404 from the server through %s as an ApiError', async (_m, call, path) => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    __http.reply(`GET ${path}`, { status: 404, json: { detail: 'not found' } });

    const calling = call();

    await expect(calling).rejects.toBeInstanceOf(ApiError);
    await expect(calling).rejects.toMatchObject({ status: 404 });
  });

  it('reaches spies installed on the api-client after the port was imported', async () => {
    const search = { results: [] } as unknown as discoveryApi.DiscoverySearchResponse;
    const suggest = { suggestions: [] };
    const history = { items: [], total: 0 };
    jest.spyOn(discoveryApi, 'searchDiscovery').mockResolvedValue(search);
    jest.spyOn(discoveryApi, 'suggestDiscovery').mockResolvedValue(suggest);
    jest.spyOn(discoveryApi, 'listSearchHistory').mockResolvedValue(history);

    await expect(httpDiscoverPort.searchDiscovery({ q: 'q' })).resolves.toBe(search);
    await expect(httpDiscoverPort.suggestDiscovery({ q: 'q' })).resolves.toBe(suggest);
    await expect(httpDiscoverPort.listSearchHistory()).resolves.toBe(history);
  });
});

describe('createMemoryDiscoverPort seeding', () => {
  it('serves empty results, suggestions and history when created with no seed', async () => {
    const port = createMemoryDiscoverPort();

    const search = await port.searchDiscovery({ q: 'q' });
    const suggest = await port.suggestDiscovery({ q: 'q' });
    const history = await port.listSearchHistory();

    expect(search.results).toEqual([]);
    expect(search.total).toBe(0);
    expect(suggest.suggestions).toEqual([]);
    expect(history).toEqual({ items: [], total: 0 });
  });
});
