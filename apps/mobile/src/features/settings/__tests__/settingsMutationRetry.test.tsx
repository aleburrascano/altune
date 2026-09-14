import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { ApiError } from '@shared/api-client';
import { clearSearchHistory } from '@shared/api-client/discovery';
import { submitReport } from '@shared/api-client/feedback';
import { backfillFeaturedArtists } from '@shared/api-client/tracks';

import { useBackfillFeatured } from '../hooks/useBackfillFeatured';
import { useClearSearchHistory } from '../hooks/useClearSearchHistory';
import { useSubmitReport } from '../hooks/useSubmitReport';

// #841: mutations default to zero retries, so a transient 502 on backfill or
// clear-history failed outright. They must retry transient failures via isRetryable().

jest.mock('@shared/api-client/discovery', () => ({
  ...jest.requireActual('@shared/api-client/discovery'),
  clearSearchHistory: jest.fn(),
}));
jest.mock('@shared/api-client/tracks', () => ({
  ...jest.requireActual('@shared/api-client/tracks'),
  backfillFeaturedArtists: jest.fn(),
}));
jest.mock('@shared/api-client/feedback', () => ({
  ...jest.requireActual('@shared/api-client/feedback'),
  submitReport: jest.fn(),
}));

function makeClient() {
  // retryDelay only keeps the test fast; the retry decision comes from the hook.
  return new QueryClient({ defaultOptions: { mutations: { retryDelay: 0 } } });
}

function makeWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

function renderMutation<T extends { mutate: (v?: never) => void }>(useHook: () => T) {
  const hook = renderHook(useHook, { wrapper: makeWrapper(makeClient()) });
  act(() => {
    hook.result.current.mutate();
  });
  return hook;
}

const badGateway = () => new ApiError(502, 'bad gateway');

beforeEach(() => {
  jest.mocked(clearSearchHistory).mockReset();
  jest.mocked(backfillFeaturedArtists).mockReset();
  jest.mocked(submitReport).mockReset();
});

describe('settings mutations retry transient failures (#841)', () => {
  it('retries a 502 on backfill and settles as success', async () => {
    jest
      .mocked(backfillFeaturedArtists)
      .mockRejectedValueOnce(badGateway())
      .mockResolvedValueOnce({ updated: 0 } as never);

    const hook = renderMutation(useBackfillFeatured);

    await waitFor(() => expect(hook.result.current.isSuccess).toBe(true));
    expect(backfillFeaturedArtists).toHaveBeenCalledTimes(2);
    hook.unmount();
  });

  it('retries a 502 on clear-history and settles as success', async () => {
    jest
      .mocked(clearSearchHistory)
      .mockRejectedValueOnce(badGateway())
      .mockResolvedValueOnce(undefined);

    const hook = renderMutation(useClearSearchHistory);

    await waitFor(() => expect(hook.result.current.isSuccess).toBe(true));
    expect(clearSearchHistory).toHaveBeenCalledTimes(2);
    hook.unmount();
  });

  it('does not retry a permanent 4xx on clear-history', async () => {
    jest.mocked(clearSearchHistory).mockRejectedValue(new ApiError(400, 'bad request'));

    const hook = renderMutation(useClearSearchHistory);

    await waitFor(() => expect(hook.result.current.isError).toBe(true));
    expect(clearSearchHistory).toHaveBeenCalledTimes(1);
    hook.unmount();
  });

  it('keeps report submission single-shot even on a transient 502', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    jest.mocked(submitReport).mockRejectedValue(badGateway());

    const hook = renderHook(() => useSubmitReport(), { wrapper: makeWrapper(makeClient()) });
    act(() => {
      hook.result.current.mutate({} as never);
    });

    await waitFor(() => expect(hook.result.current.isError).toBe(true));
    expect(submitReport).toHaveBeenCalledTimes(1);
    hook.unmount();
  });
});
