import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { discoveryKeys } from '@shared/query';
import { runSignOutCleanups } from '@shared/session';
import { signInTestUser } from '@testing/signInTestUser';

import { useClearSearchHistory } from '@shared/searchHistory';
import { flushPromises } from '@testing/flushPromises';
import { EVENT_ROUTES } from '@testing/eventRoutes';

const { __http } = require('@testing/doubles/fetch.js');

const CLEAR_HISTORY = 'DELETE /v1/discovery/search-history';
const NO_CONTENT = 204;
const BAD_REQUEST = 400;
const BAD_GATEWAY = 502;
const OLD_HISTORY = { items: [{ query: 'old' }] };
const OTHER_KEY = ['unrelated'];

type Retry = { retry: number | false; retryDelay: number };

let queryClient: QueryClient;

beforeEach(async () => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { gcTime: Infinity }, mutations: { gcTime: Infinity } },
  });
  await signInTestUser();
  __http.reply(EVENT_ROUTES, { status: 202 });
});

afterEach(() => {
  queryClient.clear();
});

type WrapperProps = { children: React.ReactNode };

function WithQueryClient({ children }: WrapperProps) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

function renderClear(retryOptions: Retry = { retry: false, retryDelay: 0 }) {
  return renderHook(() => useClearSearchHistory({ action: 'history.clear', retryOptions }), {
    wrapper: WithQueryClient,
  }).result;
}

function history(): unknown {
  return queryClient.getQueryData(discoveryKeys.history);
}

function isStale(key: readonly unknown[]): boolean | undefined {
  return queryClient.getQueryState(key)?.isInvalidated;
}

function notAnsweredYet(): void {}

function deferred<T>() {
  let resolve: (value: T) => void = notAnsweredYet;
  const promise = new Promise<T>((settle) => {
    resolve = settle;
  });
  return { promise, resolve };
}

function startReadAnsweredLater(key: readonly unknown[]) {
  const answer = deferred<unknown>();
  void queryClient.fetchQuery({ queryKey: key, queryFn: () => answer.promise }).catch(() => {});
  return answer.resolve;
}

async function settled(): Promise<void> {
  await waitFor(() => expect(queryClient.isMutating()).toBe(0));
  await act(async () => {
    await flushPromises();
  });
}

describe('useClearSearchHistory()', () => {
  it('empties the history at once, before the server answers', async () => {
    __http.hang(CLEAR_HISTORY);
    queryClient.setQueryData(discoveryKeys.history, OLD_HISTORY);
    const result = renderClear();

    act(() => result.current.mutate());

    await waitFor(() => expect(history()).toEqual({ items: [] }));
  });

  it('stops a history read already in flight, and only that read, so it cannot refill the list', async () => {
    const release = __http.hold(CLEAR_HISTORY, { status: NO_CONTENT });
    const answerHistory = startReadAnsweredLater(discoveryKeys.history);
    const answerOther = startReadAnsweredLater(OTHER_KEY);
    const result = renderClear();

    act(() => result.current.mutate());
    await waitFor(() => expect(history()).toEqual({ items: [] }));
    answerHistory(OLD_HISTORY);
    answerOther('other data');
    await act(async () => {
      await flushPromises();
    });

    expect(history()).toEqual({ items: [] });
    expect(queryClient.getQueryData(OTHER_KEY)).toBe('other data');
    release();
    await settled();
  });

  it('on success, leaves the history empty even if a read refilled it meanwhile, and does not refetch', async () => {
    const release = __http.hold(CLEAR_HISTORY, { status: NO_CONTENT });
    queryClient.setQueryData(discoveryKeys.history, OLD_HISTORY);
    const result = renderClear();

    act(() => result.current.mutate());
    await waitFor(() => expect(history()).toEqual({ items: [] }));
    queryClient.setQueryData(discoveryKeys.history, OLD_HISTORY);
    release();
    await settled();

    expect(history()).toEqual({ items: [] });
    expect(isStale(discoveryKeys.history)).toBe(false);
  });

  it('on success, stops a history read that started while the request was out, and only that read', async () => {
    const release = __http.hold(CLEAR_HISTORY, { status: NO_CONTENT });
    const result = renderClear();

    act(() => result.current.mutate());
    await waitFor(() => expect(__http.countFor(CLEAR_HISTORY)).toBe(1));
    const answerHistory = startReadAnsweredLater(discoveryKeys.history);
    const answerOther = startReadAnsweredLater(OTHER_KEY);
    release();
    await settled();
    answerHistory(OLD_HISTORY);
    answerOther('other data');
    await settled();

    expect(history()).toEqual({ items: [] });
    expect(queryClient.getQueryData(OTHER_KEY)).toBe('other data');
  });

  it('on failure, puts the old history back, marks only it stale, and surfaces the error', async () => {
    __http.reply(CLEAR_HISTORY, { status: BAD_REQUEST });
    queryClient.setQueryData(discoveryKeys.history, OLD_HISTORY);
    queryClient.setQueryData(OTHER_KEY, 'other data');
    const result = renderClear();

    act(() => result.current.mutate());
    await waitFor(() => expect(result.current.error).toMatchObject({ name: 'ApiError' }));

    expect(result.current.error).toMatchObject({ status: BAD_REQUEST });
    expect(history()).toEqual(OLD_HISTORY);
    expect(isStale(discoveryKeys.history)).toBe(true);
    expect(isStale(OTHER_KEY)).toBe(false);
  });

  it('on failure with no history loaded, keeps the empty list and marks it stale', async () => {
    __http.reply(CLEAR_HISTORY, { status: BAD_REQUEST });
    const result = renderClear();

    act(() => result.current.mutate());
    await waitFor(() => expect(result.current.error).toMatchObject({ name: 'ApiError' }));

    expect(history()).toEqual({ items: [] });
    expect(isStale(discoveryKeys.history)).toBe(true);
  });

  it('after a sign-out, a late failure restores nothing to the next user', async () => {
    const release = __http.hold(CLEAR_HISTORY, { status: BAD_REQUEST });
    queryClient.setQueryData(discoveryKeys.history, OLD_HISTORY);
    const result = renderClear();

    act(() => result.current.mutate());
    await waitFor(() => expect(__http.countFor(CLEAR_HISTORY)).toBe(1));
    runSignOutCleanups();
    queryClient.setQueryData(discoveryKeys.history, { items: [{ query: 'next-user' }] });
    release();
    await settled();

    expect(history()).toEqual({ items: [{ query: 'next-user' }] });
    expect(isStale(discoveryKeys.history)).toBe(false);
  });

  it('after a sign-out, a late success does not empty the next user history', async () => {
    const release = __http.hold(CLEAR_HISTORY, { status: NO_CONTENT });
    const result = renderClear();

    act(() => result.current.mutate());
    await waitFor(() => expect(__http.countFor(CLEAR_HISTORY)).toBe(1));
    runSignOutCleanups();
    queryClient.setQueryData(discoveryKeys.history, { items: [{ query: 'next-user' }] });
    release();
    await settled();

    expect(history()).toEqual({ items: [{ query: 'next-user' }] });
  });

  it('retries as the caller asks', async () => {
    __http.replyOnce(CLEAR_HISTORY, { status: BAD_GATEWAY });
    __http.reply(CLEAR_HISTORY, { status: NO_CONTENT });
    const result = renderClear({ retry: 1, retryDelay: 0 });

    act(() => result.current.mutate());
    await settled();

    expect(__http.countFor(CLEAR_HISTORY)).toBe(1 + 1);
    expect(result.current.error).toBeNull();
    expect(history()).toEqual({ items: [] });
  });
});
