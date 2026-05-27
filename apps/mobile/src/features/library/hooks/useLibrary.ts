/**
 * useLibrary — paginated track list via React Query's useInfiniteQuery.
 *
 * Per ADR-0005 (mobile server state) and view-library spec AC#3 (the client
 * stops fetching when `has_more` is false).
 *
 * The pagination logic lives in pure helpers `_nextOffsetFromPage` and
 * `_flattenPages` below so they can be unit-tested without spinning up React
 * Query / a QueryClientProvider / RNTL. The hook itself is a 10-line shell
 * that delegates to those helpers + React Query.
 */

import { useInfiniteQuery } from '@tanstack/react-query';

import { getTracks } from '../../../shared/api-client/tracks';
import type { ListTracksResponse, TrackResponse } from '../../../shared/api-client/types';

const PAGE_SIZE = 50;

export type LibraryState = {
  items: TrackResponse[];
  total: number;
  isLoading: boolean;
  error: Error | null;
  hasNextPage: boolean;
  fetchNextPage: () => void;
};

/**
 * Pure helper — returns the next page's offset, or undefined when the page
 * reports `has_more: false`. React Query interprets undefined as "no more
 * pages" and stops calling `queryFn`, satisfying AC#3.
 */
export function _nextOffsetFromPage(page: ListTracksResponse): number | undefined {
  if (!page.has_more) {
    return undefined;
  }
  return page.offset + page.items.length;
}

/**
 * Pure helper — concatenates items from every loaded page in order. Order is
 * preserved because pages are appended in fetch order and items inside each
 * page are already ordered by the server (added_at DESC, id DESC).
 */
export function _flattenPages(pages: ListTracksResponse[]): TrackResponse[] {
  return pages.flatMap((p) => p.items);
}

export function useLibrary(): LibraryState {
  const query = useInfiniteQuery<ListTracksResponse>({
    queryKey: ['library'],
    queryFn: ({ pageParam }) => getTracks({ limit: PAGE_SIZE, offset: pageParam as number }),
    initialPageParam: 0,
    getNextPageParam: _nextOffsetFromPage,
  });

  return {
    items: query.data ? _flattenPages(query.data.pages) : [],
    total: query.data?.pages[0]?.total ?? 0,
    isLoading: query.isLoading,
    error: query.error as Error | null,
    hasNextPage: query.hasNextPage,
    fetchNextPage: () => {
      void query.fetchNextPage();
    },
  };
}
