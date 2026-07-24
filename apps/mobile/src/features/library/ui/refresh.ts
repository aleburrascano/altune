/**
 * Pull-to-refresh wiring shared by the four Library lists.
 *
 * The Library caches are `staleTime: Infinity` + SSE-patched (F15), so a manual
 * refresh is the only user-initiated reconcile path — the error-state Retry is
 * unreachable once data has loaded. Built once in `LibraryScreen`; each list
 * spreads it onto its FlatList.
 */
export type ListRefresh = {
  onRefresh: () => void;
  refreshing: boolean;
};
