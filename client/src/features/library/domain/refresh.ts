export type ListRefresh = {
  onRefresh: () => void;
  refreshing: boolean;
};

export type ListPaging = {
  onEndReached: () => void;
  isFetchingNextPage: boolean;
  nextPageFailed: boolean;
  onRetryNextPage: () => void;
};
