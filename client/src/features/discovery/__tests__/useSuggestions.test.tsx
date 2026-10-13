import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { NetworkError } from '@shared/apiClient';
import { CORRELATION_HEADER } from '@shared/apiClient/correlationId';
import { transientRetryOptions } from '@shared/query/retryDelay';
import { MIN_QUERY_LENGTH } from '@features/discovery/domain/searchPaging';
import {
  SUGGEST_DEBOUNCE_MS,
  useAutocompleteSuggestions,
  useSuggestionVisibility,
} from '@features/discovery/hooks/useSuggestions';
import { signInTestUser } from '@testing/signInTestUser';
import { FeaturePorts } from '@/chrome/FeaturePorts';
import { httpFeaturePorts } from '@/chrome/httpFeaturePorts';

const { __http } = require('@testing/doubles/fetch.js');

const SUGGEST = 'GET /v1/discovery/suggest';
const EVENTS = 'POST /v1/discovery/events';
const EVENTS_PATH = '/v1/discovery/events';

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

beforeEach(async () => {
  jest.useFakeTimers();
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  await signInTestUser();
  __http.reply(EVENTS, { status: 204 });
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
    await waitFor(() =>
      expect(rendered.result.current.data?.suggestions.map((s) => s.text)).toEqual(['radiohead']),
    );
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
    return (__http.requests as { path: string; body: string }[])
      .filter((request) => request.path === EVENTS_PATH)
      .map(
        (request) =>
          JSON.parse(request.body) as { type: string; payload?: Record<string, unknown> },
      )
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
    __http.reply(EVENTS, { status: 204 });
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
      payload: {
        source: 'suggest',
        correlationId: sent?.headers[CORRELATION_HEADER],
        session_id: expect.any(String),
      },
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

describe('useSuggestionVisibility allows suggestions only while the user is typing', () => {
  function setup(inputValue: string) {
    const search = {
      inputValue,
      onChangeText: jest.fn(),
      onSubmit: jest.fn(),
      setQuery: jest.fn(),
    };
    const hook = renderHook(() => useSuggestionVisibility(search));
    return { search, ...hook };
  }

  it('requires focus and a minimum-length query', () => {
    const { result } = setup('a'.repeat(MIN_QUERY_LENGTH));
    expect(result.current.canShowSuggestions).toBe(false);

    act(() => result.current.setIsFocused(true));
    expect(result.current.canShowSuggestions).toBe(true);
  });

  it('stays closed for a focused query left below the minimum length by trimming', () => {
    const { result } = setup(` ${'a'.repeat(MIN_QUERY_LENGTH - 1)} `);

    act(() => result.current.setIsFocused(true));

    expect(result.current.canShowSuggestions).toBe(false);
  });

  it('hides on submit and on suggestion select, and reopens on typing', () => {
    const { result, search } = setup('radio');
    act(() => result.current.setIsFocused(true));

    act(() => result.current.onSubmit());
    expect(search.onSubmit).toHaveBeenCalledTimes(1);
    expect(result.current.canShowSuggestions).toBe(false);

    act(() => result.current.onChangeText('radioh'));
    expect(search.onChangeText).toHaveBeenCalledWith('radioh');
    expect(result.current.canShowSuggestions).toBe(true);

    act(() => result.current.onSuggestionSelect('radiohead'));
    expect(search.setQuery).toHaveBeenCalledWith('radiohead');
    expect(result.current.canShowSuggestions).toBe(false);
  });
});
