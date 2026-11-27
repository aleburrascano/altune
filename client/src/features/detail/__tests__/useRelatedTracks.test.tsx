import type { QueryClient } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';
import type { DiscoverySource } from '@shared/catalog';
import { _resetDetailHealthForTest, useRelatedTracks } from '@features/detail';
import { createTestQueryClient, createWrapper, signInTestUser } from '@testing/queryHarness';
import { freezeDate } from '@testing/freezeDate';
import { flushPromises } from '@testing/flushPromises';

const CLOCK_START = 1_700_000_000_000;

const { __http } = require('@testing/doubles/fetch.js');

afterEach(() => {
  jest.useRealTimers();
});

describe('aborting on unmount', () => {
  let client: QueryClient;

  let wrapper: ReturnType<typeof createWrapper>;

  beforeEach(async () => {
    await signInTestUser();
    __http.replyAll({ status: 204 });
    _resetDetailHealthForTest();
    client = createTestQueryClient();
    wrapper = createWrapper(client);
  });

  afterEach(() => client.clear());

  describe.each([
    [
      'useRelatedTracks',
      'GET /v1/discovery/tracks/soundcloud/sc-1/related',
      (): void =>
        void useRelatedTracks({
          sources: [{ provider: 'soundcloud', external_id: 'sc-1' } as DiscoverySource],
        }),
    ],
  ] as const)('%s, left before its request resolves', (_name, spec, useHook) => {
    it('aborts the in-flight request when the screen unmounts', async () => {
      __http.hang(spec);
      const { unmount } = renderHook(useHook, { wrapper });
      await waitFor(() => expect(__http.last()).toBeDefined());
      const { signal } = __http.last() as { signal: AbortSignal };
      expect(signal.aborted).toBe(false);

      unmount();

      await waitFor(() => expect(signal.aborted).toBe(true));
    });
  });
});

describe('reopening related tracks against the 30-minute content cache window', () => {
  const RELATED_PATH = 'GET /v1/discovery/tracks/soundcloud/sc-cache/related';
  const sources = [{ provider: 'soundcloud', external_id: 'sc-cache' } as DiscoverySource];
  const { act } = require('@testing-library/react-native');

  beforeEach(async () => {
    await signInTestUser();
    __http.replyAll({ status: 204 });
    _resetDetailHealthForTest();
  });

  it.each([
    ['serves the cached list without a request 1 ms before 30 minutes', 1_799_999, 1],
    ['asks the server again 1 ms after 30 minutes', 1_800_001, 2],
  ])('%s', async (_behaviour, elapsedMs, expectedRequests) => {
    const clock = freezeDate(CLOCK_START);
    __http.reply(RELATED_PATH, {
      status: 200,
      json: { items: [], provider_name: 'soundcloud', status: 'ok' },
    });
    const client = createTestQueryClient();
    const wrapper = createWrapper(client);

    const first = renderHook(() => useRelatedTracks({ sources }), { wrapper });
    await waitFor(() => expect(first.result.current.isLoading).toBe(false));
    expect(first.result.current.isError).toBe(false);
    first.unmount();
    clock.advance(elapsedMs);
    renderHook(() => useRelatedTracks({ sources }), { wrapper });
    await act(async () => {
      await flushPromises();
    });

    expect(__http.countFor(RELATED_PATH)).toBe(expectedRequests);
    client.clear();
  });
});

describe('useRelatedTracks with no SoundCloud source', () => {
  const { act } = require('@testing-library/react-native');

  it('sends nothing, shows no loading and reports no failure', async () => {
    await signInTestUser();
    const { result } = renderHook(
      () => useRelatedTracks({ sources: [{ provider: 'spotify', external_id: 'sp-1', url: '' }] }),
      { wrapper: createWrapper(createTestQueryClient()) },
    );
    await act(async () => {
      await flushPromises();
    });

    expect(__http.requests).toHaveLength(0);
    expect(result.current.isLoading).toBe(false);
    expect(result.current.failure).toBeNull();
  });
});
