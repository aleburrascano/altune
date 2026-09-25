import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { act } from '@testing-library/react-native';
import { applyKillSwitches } from '@shared/killSwitch/killSwitch';

import { useResolveMissingSources } from '../hooks/useResolveMissingSources';

const mockQueryFn = jest.fn<Promise<DiscoveryResult[]>, []>();

jest.mock('../resolve-entity-query', () => ({
  resolveEntityQuery: (kind: string, q: string, limit: number) => ({
    queryKey: ['resolve-entity', kind, q, limit],
    queryFn: () => mockQueryFn(),
  }),
}));

function createWrapper() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

function track(overrides: Partial<DiscoveryResult> = {}): DiscoveryResult {
  return {
    kind: 'track',
    title: 'Karma Police',
    subtitle: 'Radiohead',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: {},
    ...overrides,
  };
}

const source = {
  provider: 'youtube',
  external_id: 'abc',
} as unknown as DiscoveryResult['sources'][number];

let warnSpy: jest.SpyInstance;

beforeEach(() => {
  mockQueryFn.mockReset();
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
});

afterEach(() => {
  warnSpy.mockRestore();
});

describe('useResolveMissingSources', () => {
  it('returns the result untouched without querying when it already has sources', () => {
    const input = track({ sources: [source] });
    const { result } = renderHook(() => useResolveMissingSources(input), {
      wrapper: createWrapper(),
    });

    expect(result.current).toEqual({ resolved: input, isResolving: false });
    expect(mockQueryFn).not.toHaveBeenCalled();
  });

  it('backfills sources from the exact title and artist match, keeping its own extras on top', async () => {
    mockQueryFn.mockResolvedValue([
      track({ title: 'Karma Police', subtitle: 'Someone Else', sources: [source] }),
      track({
        title: ' karma police ',
        subtitle: 'RADIOHEAD',
        sources: [source],
        extras: { a: 1, b: 1 },
      }),
    ]);
    const input = track({ extras: { b: 2 } });
    const { result } = renderHook(() => useResolveMissingSources(input), {
      wrapper: createWrapper(),
    });

    expect(result.current.isResolving).toBe(true);
    await waitFor(() => expect(result.current.resolved.sources).toEqual([source]));
    expect(result.current.resolved.extras).toEqual({ a: 1, b: 2 });
    expect(result.current.isResolving).toBe(false);
  });

  it('keeps the original result when no candidate matches', async () => {
    mockQueryFn.mockResolvedValue([track({ title: 'Paranoid Android', sources: [source] })]);
    const input = track();
    const { result } = renderHook(() => useResolveMissingSources(input), {
      wrapper: createWrapper(),
    });

    await waitFor(() => expect(result.current.isResolving).toBe(false));
    expect(result.current.resolved).toBe(input);
  });

  it('stays silent when the query succeeds with no match', async () => {
    mockQueryFn.mockResolvedValue([]);
    const { result } = renderHook(() => useResolveMissingSources(track()), {
      wrapper: createWrapper(),
    });

    await waitFor(() => expect(result.current.isResolving).toBe(false));
    expect(warnSpy).not.toHaveBeenCalled();
  });

  it('stops resolving once a failed query settles', async () => {
    mockQueryFn.mockRejectedValue(new Error('network down'));
    const input = track();
    const { result } = renderHook(() => useResolveMissingSources(input), {
      wrapper: createWrapper(),
    });

    await waitFor(() => expect(result.current.isResolving).toBe(false));
    expect(result.current.resolved).toBe(input);
  });

  it('logs the kind, title and artist it was resolving when the query fails', async () => {
    mockQueryFn.mockRejectedValue(new Error('network down'));
    renderHook(() => useResolveMissingSources(track()), { wrapper: createWrapper() });

    await waitFor(() =>
      expect(warnSpy).toHaveBeenCalledWith('[detail] source resolution fetch failed', {
        kind: 'track',
        title: 'Karma Police',
        subtitle: 'Radiohead',
        error: 'network down',
      }),
    );
  });

  it('does not query while the detail kill switch is off, and queries once it is back on', async () => {
    mockQueryFn.mockResolvedValue([]);
    act(() => applyKillSwitches({ detail_enrichment_enabled: false }));
    const { result } = renderHook(() => useResolveMissingSources(track()), {
      wrapper: createWrapper(),
    });

    expect(mockQueryFn).not.toHaveBeenCalled();
    expect(result.current.isResolving).toBe(false);

    act(() => applyKillSwitches({ detail_enrichment_enabled: true }));

    await waitFor(() => expect(mockQueryFn).toHaveBeenCalledTimes(1));
  });
});
