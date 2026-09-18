import { act, renderHook } from '@testing-library/react-native';

import { createElement, type ReactElement, type ReactNode } from 'react';

import { DetailHandoffProvider } from '../handoff-context';
import { useReportWrongAlbum } from '../hooks/useReportWrongAlbum';

import type { DiscoveryResult } from '@shared/api-client/discovery';

const mockEnqueueCritical = jest.fn();

jest.mock('@shared/telemetry/outbox', () => ({
  enqueueCritical: (...args: unknown[]) => mockEnqueueCritical(...args),
}));

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

beforeEach(() => {
  mockEnqueueCritical.mockClear();
});

describe('useReportWrongAlbum guards against double submits', () => {
  it('enqueues only one report when the action fires twice synchronously', () => {
    const { result } = renderHook(() => useReportWrongAlbum(resultFixture()), {
      wrapper: withHandoff('search-1'),
    });

    act(() => {
      result.current.report();
      result.current.report();
    });

    expect(mockEnqueueCritical).toHaveBeenCalledTimes(1);
  });

  it('enqueues a wrong_album event with the result identity', () => {
    const { result } = renderHook(() => useReportWrongAlbum(resultFixture()), {
      wrapper: withHandoff('search-1'),
    });

    act(() => {
      result.current.report();
    });

    expect(mockEnqueueCritical).toHaveBeenCalledWith({
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
  it('reports an empty album tag as no album rather than as an empty name', () => {
    const { result } = renderHook(
      () => useReportWrongAlbum(resultFixture({ extras: { album: '' } })),
      { wrapper: withHandoff('search-1') },
    );

    act(() => {
      result.current.report();
    });

    expect(mockEnqueueCritical).toHaveBeenCalledWith(
      expect.objectContaining({
        payload: expect.objectContaining({ album: null }),
      }),
    );
  });
});
