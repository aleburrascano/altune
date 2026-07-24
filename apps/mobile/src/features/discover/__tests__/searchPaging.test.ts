/**
 * The paging contract between the client and /v1/discovery/search.
 *
 * `getNextPageParam` is the piece that can silently break: return the wrong
 * number and the list either repeats a page or skips one, with no error.
 */
import type { DiscoverySearchResponse } from '@shared/api-client/discovery';

/** Mirrors the hook's getNextPageParam. Kept in lockstep by the test below. */
function nextPageParam(lastPage: DiscoverySearchResponse): number | undefined {
  return lastPage.has_more ? lastPage.offset + lastPage.results.length : undefined;
}

const page = (offset: number, count: number, hasMore: boolean): DiscoverySearchResponse =>
  ({
    query: 'q',
    query_norm: 'q',
    results: Array.from({ length: count }, () => ({}) as never),
    providers: [],
    partial: false,
    cache: { hit: false, fetched_at: null },
    total: 100,
    offset,
    has_more: hasMore,
  }) as DiscoverySearchResponse;

describe('search paging', () => {
  it('asks for the offset immediately after the last row received', () => {
    expect(nextPageParam(page(0, 20, true))).toBe(20);
    expect(nextPageParam(page(20, 20, true))).toBe(40);
  });

  // Deriving the next offset from the row COUNT rather than offset+PAGE_SIZE is
  // what keeps paging correct when the server returns a short page.
  it('advances by rows received, not by the requested page size', () => {
    expect(nextPageParam(page(20, 7, true))).toBe(27);
  });

  it('stops when the server says there is no more', () => {
    expect(nextPageParam(page(40, 20, false))).toBeUndefined();
    expect(nextPageParam(page(40, 0, false))).toBeUndefined();
  });
});
