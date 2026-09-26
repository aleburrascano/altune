import { act, renderHook } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { useDiscographyFilter } from '../hooks/useDiscographyFilter';

function album(title: string, recordType: string): DiscoveryResult {
  return {
    kind: 'album',
    title,
    subtitle: null,
    image_url: null,
    confidence: 'high',
    sources: [{ provider: 'test', external_id: title, url: `https://altune.test/${title}` }],
    extras: { record_type: recordType },
  };
}

describe('useDiscographyFilter(): chip selection', () => {
  it('defaults to the first present record type and switches active on select', () => {
    const albums = [album('A1', 'album'), album('S1', 'single')];
    const { result } = renderHook(() => useDiscographyFilter(albums));

    expect(result.current?.active.type).toBe('album');
    expect(result.current?.present.map((p) => p.type)).toEqual(['album', 'single']);

    act(() => {
      result.current?.select('single');
    });

    expect(result.current?.active.type).toBe('single');
    expect(result.current?.items).toHaveLength(1);
    expect(result.current?.items[0]?.title).toBe('S1');
  });

  it('returns null when no albums match a known record type', () => {
    const { result } = renderHook(() => useDiscographyFilter([]));

    expect(result.current).toBeNull();
  });
});

describe('useDiscographyFilter(): the cap and "see all" expansion', () => {
  it('caps items at 10 until expand() is called, then reveals the rest', () => {
    const albums = Array.from({ length: 15 }, (_unused, i) => album(`Album ${i}`, 'album'));
    const { result } = renderHook(() => useDiscographyFilter(albums));

    expect(result.current?.capped).toHaveLength(10);
    expect(result.current?.hasMore).toBe(true);

    act(() => {
      result.current?.expand();
    });

    expect(result.current?.capped).toHaveLength(15);
    expect(result.current?.hasMore).toBe(false);
  });

  it('resets expansion when a new record type is selected', () => {
    const albums = [
      ...Array.from({ length: 15 }, (_unused, i) => album(`Album ${i}`, 'album')),
      album('Single 1', 'single'),
    ];
    const { result } = renderHook(() => useDiscographyFilter(albums));

    act(() => {
      result.current?.expand();
    });
    expect(result.current?.hasMore).toBe(false);

    act(() => {
      result.current?.select('single');
    });

    expect(result.current?.active.type).toBe('single');
    expect(result.current?.hasMore).toBe(false);
  });
});
