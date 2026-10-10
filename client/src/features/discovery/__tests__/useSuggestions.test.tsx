import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { NetworkError } from '@shared/api-client';
import { supabase } from '@shared/auth/supabaseClient';
import { CORRELATION_HEADER } from '@shared/api-client/correlationId';
import { transientRetryOptions } from '@shared/query/retryDelay';
import { recordEvent } from '@shared/telemetry/recordEvent';
import { MIN_QUERY_LENGTH } from '@features/discovery/domain/searchPaging';
import {
  SUGGEST_DEBOUNCE_MS,
  useAutocompleteSuggestions,
  useSuggestionVisibility,
} from '@features/discovery/hooks/useSuggestions';
import { FeaturePorts } from '@/chrome/FeaturePorts';
import { httpFeaturePorts } from '@/chrome/httpFeaturePorts';

const { __http } = require('@testing/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));
jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

const mockRecordEvent = recordEvent as jest.Mock;

const SUGGEST = 'GET /v1/discovery/suggest';

type SentRequest = {
  path: string;
  query: string;
  headers: Record<string, string>;
  signal?: AbortSignal;
};

function suggestRequests(): SentRequest[] {
  return (__http.requests as SentRequest[]).filter(
    (request) => request.path === '/v1/discovery/suggest',
  );
}

function serveSuggestions(texts: string[]): void {
  __http.reply(SUGGEST, {
    json: { suggestions: texts.map((text) => ({ text, kind: 'query', popularity: 0 })) },
  });
}

let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      <FeaturePorts ports={httpFeaturePorts}>{children}</FeaturePorts>
    </QueryClientProvider>
  );
}

function renderSuggestions() {
  return renderHook(({ text }: { text: string }) => useAutocompleteSuggestions(text), {
    wrapper,
    initialProps: { text: '' },
  });
}

type Rendered = ReturnType<typeof renderSuggestions>;

function typeBurst(rendered: Rendered, text: string): void {
  for (let length = 1; length <= text.length; length += 1) {
    act(() => {
      rendered.rerender({ text: text.slice(0, length) });
      jest.advanceTimersByTime(SUGGEST_DEBOUNCE_MS - 1);
    });
  }
}

function settle(): void {
  act(() => {
    jest.advanceTimersByTime(SUGGEST_DEBOUNCE_MS);
  });
}

function signalOfRequest(index: number): AbortSignal | undefined {
  return suggestRequests()[index]?.signal;
}

beforeEach(() => {
  jest.useFakeTimers();
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
  serveSuggestions(['radiohead']);
});

afterEach(() => {
  queryClient.clear();
  jest.runOnlyPendingTimers();
  jest.useRealTimers();
  jest.restoreAllMocks();
});

describe('useAutocompleteSuggestions asks for suggestions once the typing settles', () => {
  it('sends one request for a burst of keystrokes typed inside the debounce window', async () => {
    const rendered = renderSuggestions();

    typeBurst(rendered, 'radiohead');
    settle();

    await waitFor(() =>
      expect(rendered.result.current.data?.suggestions.map((s) => s.text)).toEqual(['radiohead']),
    );
    expect(suggestRequests().map((request) => request.query)).toEqual(['q=radiohead&limit=5']);
  });

  it('holds the request back until the typing has paused for the debounce window', () => {
    const rendered = renderSuggestions();

    typeBurst(rendered, 'rad');

    expect(suggestRequests()).toHaveLength(0);
    expect(rendered.result.current.data).toBeUndefined();
  });
});

describe('useAutocompleteSuggestions keys a query by its canonical spacing', () => {
  it('sends one suggest request for "A  b" and "A b"', async () => {
    const rendered = renderSuggestions();

    act(() => {
      rendered.rerender({ text: 'A  b' });
    });
    settle();
    await waitFor(() => expect(rendered.result.current.data).toBeDefined());
    act(() => {
      rendered.rerender({ text: 'A b' });
    });
    await act(async () => {
      await jest.advanceTimersByTimeAsync(SUGGEST_DEBOUNCE_MS);
    });

    expect(suggestRequests().map((request) => request.query)).toEqual(['q=a+b&limit=5']);
  });
});

describe('useAutocompleteSuggestions cancels a suggest request its successor supersedes', () => {
  it('aborts the in-flight request when a later query starts', async () => {
    __http.reset();
    __http.hang(SUGGEST);
    const rendered = renderSuggestions();

    typeBurst(rendered, 'rad');
    settle();
    await waitFor(() => expect(suggestRequests()).toHaveLength(1));
    typeBurst(rendered, 'radiohead');
    settle();
    await waitFor(() => expect(suggestRequests()).toHaveLength(['rad', 'radiohead'].length));

    expect(suggestRequests().map((request) => request.query)).toEqual([
      'q=rad&limit=5',
      'q=radiohead&limit=5',
    ]);
    expect(signalOfRequest(0)?.aborted).toBe(true);
    expect(signalOfRequest(1)?.aborted).toBe(false);
  });
});

describe('discover query failures emit a search_failed telemetry event tagged with its source', () => {
  function failureEvents() {
    return mockRecordEvent.mock.calls
      .map((call) => call[0] as { type: string; payload?: Record<string, unknown> })
      .filter((event) => event.type === 'search_failed');
  }

  beforeEach(() => {
    jest.useRealTimers();
  });

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    __http.reset();
    mockRecordEvent.mockReset().mockResolvedValue(undefined);
  });

  afterEach(() => {
    queryClient.clear();
  });

  afterEach(() => {
    jest.useFakeTimers();
  });

  it('a failed suggest fires search_failed with source suggest, omitting status for non-HTTP errors', async () => {
    __http.fail(SUGGEST);
    const { result } = renderHook(() => useAutocompleteSuggestions('rad'), { wrapper });

    await waitFor(() => expect(result.current.error).not.toBeNull());
    await waitFor(() => expect(failureEvents()).toHaveLength(1));

    const [sent] = suggestRequests();
    expect(failureEvents()[0]).toEqual({
      type: 'search_failed',
      payload: { source: 'suggest', correlationId: sent?.headers[CORRELATION_HEADER] },
    });
  });
});

describe('useAutocompleteSuggestions does not retry a failed suggest request', () => {
  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { ...transientRetryOptions }, mutations: { retry: false } },
    });
  });

  it('makes one attempt when the request fails with a NetworkError', async () => {
    __http.reset();
    __http.fail(SUGGEST);
    const rendered = renderSuggestions();

    typeBurst(rendered, 'rad');
    settle();
    await act(async () => {
      await jest.advanceTimersByTimeAsync(60_000);
    });

    expect(rendered.result.current.error).toBeInstanceOf(NetworkError);
    expect(suggestRequests()).toHaveLength(1);
  });
});

describe('useSuggestionVisibility shows suggestions only while the user is typing', () => {
  function setup(inputValue: string, count: number) {
    const search = {
      inputValue,
      onChangeText: jest.fn(),
      onSubmit: jest.fn(),
      setQuery: jest.fn(),
    };
    const hook = renderHook(({ n }: { n: number }) => useSuggestionVisibility(search, n), {
      initialProps: { n: count },
    });
    return { search, ...hook };
  }

  it('requires focus, a minimum-length query and at least one suggestion', () => {
    const { result, rerender } = setup('a'.repeat(MIN_QUERY_LENGTH), 1);
    expect(result.current.showSuggestions).toBe(false);

    act(() => result.current.setIsFocused(true));
    expect(result.current.showSuggestions).toBe(true);

    rerender({ n: 0 });
    expect(result.current.showSuggestions).toBe(false);
  });

  it('stays closed for a focused query left below the minimum length by trimming', () => {
    const { result } = setup(` ${'a'.repeat(MIN_QUERY_LENGTH - 1)} `, 1);

    act(() => result.current.setIsFocused(true));

    expect(result.current.showSuggestions).toBe(false);
  });

  it('hides on submit and on suggestion select, and reopens on typing', () => {
    const { result, search } = setup('radio', 3);
    act(() => result.current.setIsFocused(true));

    act(() => result.current.onSubmit());
    expect(search.onSubmit).toHaveBeenCalled();
    expect(result.current.showSuggestions).toBe(false);

    act(() => result.current.onChangeText('radioh'));
    expect(search.onChangeText).toHaveBeenCalledWith('radioh');
    expect(result.current.showSuggestions).toBe(true);

    act(() => result.current.onSuggestionSelect('radiohead'));
    expect(search.setQuery).toHaveBeenCalledWith('radiohead');
    expect(result.current.showSuggestions).toBe(false);
  });
});
