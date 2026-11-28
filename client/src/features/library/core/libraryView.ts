import { asyncView, type AsyncView } from '@shared/ui';
import type { LibrarySort } from '@features/library/core/libraryGroups';

export type LibraryChip = 'playlists' | 'tracks' | 'albums' | 'artists';

export type SortKey = LibrarySort;

export const ALBUM_SORT_OPTIONS: { key: SortKey; label: string }[] = [
  { key: 'recent', label: 'Recent' },
  { key: 'az', label: 'A–Z' },
  { key: 'year', label: 'Year' },
];

export const ARTIST_SORT_OPTIONS: { key: SortKey; label: string }[] = [
  { key: 'recent', label: 'Recent' },
  { key: 'az', label: 'A–Z' },
];

export const TRACK_SORT_OPTIONS: { key: SortKey; label: string }[] = [
  { key: 'recent', label: 'Recent' },
  { key: 'az', label: 'A–Z' },
  { key: 'year', label: 'Year' },
];

export const PLAYLIST_SORT_OPTIONS: { key: SortKey; label: string }[] = [
  { key: 'recent', label: 'Recent' },
  { key: 'az', label: 'A–Z' },
];

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

type LibrarySectionInput = {
  isLoading: boolean;
  error: unknown;
  count: number;
  showEmpty: boolean;
};

export function librarySection(input: LibrarySectionInput): AsyncView {
  const { isLoading, error, count, showEmpty } = input;
  return asyncView({ isLoading, isError: Boolean(error), isEmpty: count === 0 && showEmpty });
}
