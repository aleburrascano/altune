import { useQuery } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import type { DetailPort } from '@/features/detail/ports/detailPort';
import { resolveEntityQuery } from '@/features/detail/domain/resolveEntityQuery';
import { createMemoryDetailPort } from '@/features/detail/__tests__/memoryDetailPort';
import {
  createTestQueryClient,
  createWrapper,
} from '@/features/detail/__tests__/support/queryHarness';

function artist(title: string): DiscoveryResult {
  return {
    kind: 'artist',
    title,
    subtitle: null,
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: {},
  };
}

function render(port: DetailPort, q: string) {
  const client = createTestQueryClient();
  const hook = renderHook(() => useQuery(resolveEntityQuery('artist', q, 1, port)), {
    wrapper: createWrapper(client),
  });
  return { client, ...hook };
}

describe('resolveEntityQuery', () => {
  it('returns the fake discovery results for a seeded name', async () => {
    const port = createMemoryDetailPort({ discovery: [artist('Radiohead')] });
    const { result } = render(port, 'radiohead');
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual([artist('Radiohead')]);
  });

  it('returns [] for an unseeded name', async () => {
    const { result } = render(createMemoryDetailPort(), 'nobody');
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual([]);
  });

  it('keeps the port out of the query key', () => {
    const a = resolveEntityQuery('artist', 'radiohead', 1, createMemoryDetailPort());
    const b = resolveEntityQuery(
      'artist',
      'radiohead',
      1,
      createMemoryDetailPort({ discovery: [artist('x')] }),
    );
    expect(a.queryKey).toEqual(['resolve-entity', 'artist', 'radiohead', 1]);
    expect(b.queryKey).toEqual(a.queryKey);
  });

  it('aborts the in-flight search when the screen unmounts', async () => {
    let seen: AbortSignal | undefined;
    const port: DetailPort = {
      ...createMemoryDetailPort(),
      searchDiscovery: (_params, signal) => {
        seen = signal;
        return new Promise(() => {});
      },
    };
    const { unmount } = render(port, 'radiohead');
    await waitFor(() => expect(seen).toBeDefined());
    expect(seen?.aborted).toBe(false);

    unmount();

    await waitFor(() => expect(seen?.aborted).toBe(true));
  });
});
