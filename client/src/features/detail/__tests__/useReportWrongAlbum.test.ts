import { act, renderHook, waitFor } from '@testing-library/react-native';

import { createElement, type ReactElement, type ReactNode } from 'react';

import { DetailHandoffProvider } from '@features/detail/hooks/useDetailHandoff';
import { useReportWrongAlbum } from '@features/detail/hooks/useReportWrongAlbum';

import type { DiscoveryResult } from '@shared/apiClient/discovery';

import { signInTestUser } from '@testing/queryHarness';

const { __http } = require('../../../../jest/doubles/fetch.js');

const EVENTS = 'POST /v1/discovery/events';

function postedEvents(): Record<string, unknown>[] {
  return (__http.requests as { method: string; path: string; body: string }[])
    .filter((request) => request.method === 'POST' && request.path === '/v1/discovery/events')
    .map((request) => JSON.parse(request.body) as Record<string, unknown>);
}

function withHandoff(searchId: string | null) {
  return function Wrapper({ children }: { children: ReactNode }): ReactElement {
    return createElement(
      DetailHandoffProvider,
      { value: { result: resultFixture(), searchId } },
      children,
    );
  };
}

function resultFixture(overrides: Partial<DiscoveryResult> = {}): DiscoveryResult {
  return {
    kind: 'album',
    title: 'The Title',
    subtitle: 'The Artist',
    image_url: null,
    confidence: 'high',
    result_signature: 'sig',
    sources: [{ provider: 'spotify', external_id: 'ext-1', url: 'https://x' }],
    extras: { album: 'Wrong Album' },
    ...overrides,
  };
}

beforeEach(async () => {
  await signInTestUser();
  __http.replyAll({ status: 204 });
});

describe('useReportWrongAlbum guards against double submits', () => {
  it('enqueues only one report when the action fires twice synchronously', async () => {
    const { result } = renderHook(() => useReportWrongAlbum(resultFixture()), {
      wrapper: withHandoff('search-1'),
    });

    act(() => {
      result.current.report();
      result.current.report();
    });

    await waitFor(() => expect(__http.countFor(EVENTS)).toBe(1));
    await act(async () => {
      await new Promise((resolve) => setImmediate(resolve));
    });
    expect(__http.countFor(EVENTS)).toBe(1);
  });

  it('enqueues a wrong_album event with the result identity', async () => {
    const { result } = renderHook(() => useReportWrongAlbum(resultFixture()), {
      wrapper: withHandoff('search-1'),
    });

    act(() => {
      result.current.report();
    });

    await waitFor(() => expect(postedEvents()).toHaveLength(1));
    expect(postedEvents()[0]).toMatchObject({
      type: 'wrong_album',
      search_id: 'search-1',
      payload: {
        kind: 'album',
        title: 'The Title',
        subtitle: 'The Artist',
        album: 'Wrong Album',
        result_signature: 'sig',
      },
    });
  });
});

describe('useReportWrongAlbum reports the album it was shown', () => {
  it('reports an empty album tag as no album rather than as an empty name', async () => {
    const { result } = renderHook(
      () => useReportWrongAlbum(resultFixture({ extras: { album: '' } })),
      { wrapper: withHandoff('search-1') },
    );

    act(() => {
      result.current.report();
    });

    await waitFor(() => expect(postedEvents()).toHaveLength(1));
    expect(postedEvents()[0]).toMatchObject({ payload: { album: null } });
  });
});
