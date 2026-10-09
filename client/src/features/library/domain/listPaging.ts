export const GROUP_PAGE_SIZE = 50;

export function nextGroupPageOffset(
  pageLength: number,
  pageOffset: number,
  pageSize: number = GROUP_PAGE_SIZE,
): number | undefined {
  return pageLength < pageSize ? undefined : pageOffset + pageLength;
}

interface PagedListState {
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  isFetchNextPageError: boolean;
  fetchNextPage: () => unknown;
  refetch: () => unknown;
}

const canFetchMore = (s: PagedListState) =>
  s.hasNextPage && !s.isFetchingNextPage && !s.isFetchNextPageError;

export function pagedListControls(state: PagedListState) {
  return {
    onEndReached: () => {
      if (canFetchMore(state)) void state.fetchNextPage();
    },
    refetch: () => {
      void state.refetch();
    },
  };
}
