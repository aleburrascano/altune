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
