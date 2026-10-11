import { renderHook, waitFor, act } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/apiClient/discovery';

import { applyKillSwitches } from '@shared/killSwitch/killSwitch';

import { useResolveMissingSources } from '@/features/detail/hooks/useResolveMissingSources';
import type { DetailPort } from '@/features/detail/ports/detailPort';
import { createMemoryDetailPort } from '@/features/detail/__tests__/memoryDetailPort';
import {
  createPortWrapper,
  createTestQueryClient,
} from '@/features/detail/__tests__/support/queryHarness';

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

type Search = { q: string; kinds: readonly string[] | undefined };
type Searches = Search[];
type CountingPort = { port: DetailPort; searches: Searches };

function countingPort(port: DetailPort): CountingPort {
  const searches: Searches = [];
  return {
    searches,
    port: {
      ...port,
      searchDiscovery: (params, signal) => {
        searches.push({ q: params.q, kinds: params.kinds });
        return port.searchDiscovery(params, signal);
      },
    },
  };
}

function seeded(discovery: DiscoveryResult[]) {
  return countingPort(createMemoryDetailPort({ discovery }));
}

function failing(message: string) {
  return countingPort({
    ...createMemoryDetailPort(),
    searchDiscovery: () => Promise.reject(new Error(message)),
  });
}

function wrapperFor(port: DetailPort) {
  return createPortWrapper(createTestQueryClient(), { detail: port });
}

let warnSpy: jest.SpyInstance;

beforeEach(() => {
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
});

afterEach(() => {
  warnSpy.mockRestore();
});

describe('useResolveMissingSources', () => {
  it('returns the result untouched without querying when it already has sources', () => {
    const { port, searches } = seeded([]);
    const input = track({ sources: [source] });
    const { result } = renderHook(() => useResolveMissingSources(input), {
      wrapper: wrapperFor(port),
    });

    expect(result.current).toEqual({ resolved: input, isResolving: false });
    expect(searches).toEqual([]);
  });

  it('searches for the title and artist of the kind it is resolving', async () => {
    const { port, searches } = seeded([]);
    const { result } = renderHook(() => useResolveMissingSources(track()), {
      wrapper: wrapperFor(port),
    });

    await waitFor(() => expect(result.current.isResolving).toBe(false));
    expect(searches).toEqual([{ q: 'Karma Police Radiohead', kinds: ['track'] }]);
  });

  it('backfills sources from the exact title and artist match, keeping its own extras on top', async () => {
    const { port } = seeded([
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
      wrapper: wrapperFor(port),
    });

    expect(result.current.isResolving).toBe(true);
    await waitFor(() => expect(result.current.resolved.sources).toEqual([source]));
    expect(result.current.resolved.extras).toEqual({ a: 1, b: 2 });
    expect(result.current.isResolving).toBe(false);
  });

  it('takes the photo of the match when the result has none, and keeps its own otherwise', async () => {
    const { port } = seeded([track({ sources: [source], image_url: 'https://img/match.jpg' })]);
    const { result: bare } = renderHook(() => useResolveMissingSources(track()), {
      wrapper: wrapperFor(port),
    });
    const { result: pictured } = renderHook(
      () => useResolveMissingSources(track({ image_url: 'https://img/own.jpg' })),
      { wrapper: wrapperFor(port) },
    );

    await waitFor(() => expect(bare.current.resolved.image_url).toBe('https://img/match.jpg'));
    await waitFor(() => expect(pictured.current.resolved.sources).toEqual([source]));
    expect(pictured.current.resolved.image_url).toBe('https://img/own.jpg');
  });

  it('keeps the original result when no candidate matches', async () => {
    const { port } = seeded([track({ title: 'Paranoid Android', sources: [source] })]);
    const input = track();
    const { result } = renderHook(() => useResolveMissingSources(input), {
      wrapper: wrapperFor(port),
    });

    await waitFor(() => expect(result.current.isResolving).toBe(false));
    expect(result.current.resolved).toBe(input);
  });

  it('stays silent when the query succeeds with no match', async () => {
    const { port } = seeded([]);
    const { result } = renderHook(() => useResolveMissingSources(track()), {
      wrapper: wrapperFor(port),
    });

    await waitFor(() => expect(result.current.isResolving).toBe(false));
    expect(warnSpy).not.toHaveBeenCalled();
  });

  it('stops resolving once a failed query settles', async () => {
    const { port } = failing('network down');
    const input = track();
    const { result } = renderHook(() => useResolveMissingSources(input), {
      wrapper: wrapperFor(port),
    });

    await waitFor(() => expect(result.current.isResolving).toBe(false));
    expect(result.current.resolved).toBe(input);
  });

  it('logs the kind, title and artist it was resolving when the query fails', async () => {
    const { port } = failing('network down');
    renderHook(() => useResolveMissingSources(track()), {
      wrapper: wrapperFor(port),
    });

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
    const { port, searches } = seeded([]);
    act(() => applyKillSwitches({ detail_enrichment_enabled: false }));
    const { result } = renderHook(() => useResolveMissingSources(track()), {
      wrapper: wrapperFor(port),
    });

    expect(searches).toEqual([]);
    expect(result.current.isResolving).toBe(false);

    act(() => applyKillSwitches({ detail_enrichment_enabled: true }));

    await waitFor(() => expect(searches).toHaveLength(1));
  });
});
