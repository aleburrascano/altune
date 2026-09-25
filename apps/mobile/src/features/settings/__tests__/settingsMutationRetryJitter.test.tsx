import { QueryClient } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';

import { ApiError } from '@shared/api-client';
import { clearSearchHistory } from '@shared/api-client/discovery';
import { backfillFeaturedArtists } from '@shared/api-client/tracks';
import { RETRY_BACKOFF_BASE_MS } from '@shared/query/retryDelay';

import { makeWrapper } from '../../../../jest/makeWrapper';
import { useBackfillFeatured } from '../hooks/useBackfillFeatured';
import { useClearSearchHistory } from '../hooks/useClearSearchHistory';

// #1756: these mutations set no retryDelay, so they inherited react-query's fixed
// 1000ms first backoff and every client failing on one outage retried together.

jest.mock('@shared/api-client/discovery', () => ({
  ...jest.requireActual('@shared/api-client/discovery'),
  clearSearchHistory: jest.fn(),
}));
jest.mock('@shared/api-client/tracks', () => ({
  ...jest.requireActual('@shared/api-client/tracks'),
  backfillFeaturedArtists: jest.fn(),
}));

const retryingMutations = [
  {
    name: 'backfill',
    useMutationHook: useBackfillFeatured,
    api: () => backfillFeaturedArtists as jest.Mock,
  },
  {
    name: 'clear-history',
    useMutationHook: useClearSearchHistory,
    api: () => clearSearchHistory as jest.Mock,
  },
];

// Equal jitter puts the first retry at half the base ceiling plus the sample's
// share of the other half; un-jittered every sample would land on the ceiling.
const jitterSamples = [
  { sample: 0, dueMs: RETRY_BACKOFF_BASE_MS / 2 },
  { sample: 0.5, dueMs: (RETRY_BACKOFF_BASE_MS * 3) / 4 },
];

function startMutation(useMutationHook: () => { mutate: (v?: never) => void }) {
  const queryClient = new QueryClient();
  const hook = renderHook(useMutationHook, { wrapper: makeWrapper(queryClient) });
  act(() => {
    hook.result.current.mutate();
  });
  return () => {
    hook.unmount();
    queryClient.clear();
  };
}

async function elapse(ms: number) {
  await act(async () => {
    await jest.advanceTimersByTimeAsync(ms);
  });
}

beforeEach(() => {
  jest.useFakeTimers();
  (clearSearchHistory as jest.Mock).mockReset().mockRejectedValue(new ApiError(502, 'bad gateway'));
  (backfillFeaturedArtists as jest.Mock)
    .mockReset()
    .mockRejectedValue(new ApiError(502, 'bad gateway'));
});

afterEach(() => {
  jest.useRealTimers();
  jest.restoreAllMocks();
});

describe.each(retryingMutations)(
  '$name spreads its retry over the jittered backoff (#1756)',
  ({ useMutationHook, api }) => {
    it.each(jitterSamples)(
      'reattempts at $dueMs ms when the jitter sample is $sample',
      async ({ sample, dueMs }) => {
        jest.spyOn(Math, 'random').mockReturnValue(sample);
        const stop = startMutation(useMutationHook);

        await elapse(dueMs - 1);
        expect(api()).toHaveBeenCalledTimes(1);

        await elapse(1);
        expect(api()).toHaveBeenCalledTimes(2);

        stop();
      },
    );
  },
);
