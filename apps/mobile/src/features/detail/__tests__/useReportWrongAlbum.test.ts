import { act, renderHook } from '@testing-library/react-native';

import { useReportWrongAlbum } from '../hooks/useReportWrongAlbum';

import type { DiscoveryResult } from '@shared/api-client/discovery';

const mockEnqueueCritical = jest.fn();

jest.mock('@shared/telemetry/outbox', () => ({
  enqueueCritical: (...args: unknown[]) => mockEnqueueCritical(...args),
}));

jest.mock('@shared/lib/detail-handoff', () => ({
  getDetailHandoffSearchId: () => 'search-1',
}));

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
    const { result } = renderHook(() => useReportWrongAlbum(resultFixture()));

    act(() => {
      result.current.report();
      result.current.report();
    });

    expect(mockEnqueueCritical).toHaveBeenCalledTimes(1);
  });

  it('enqueues a wrong_album event with the result identity', () => {
    const { result } = renderHook(() => useReportWrongAlbum(resultFixture()));

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
