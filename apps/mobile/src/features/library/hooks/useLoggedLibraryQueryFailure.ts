import { useEffect } from 'react';

import { failureLogFields } from '../failureLogFields';

/** The library chips whose contents come from a query that can fail. */
export type LibraryQueryChip = 'tracks' | 'albums' | 'artists' | 'playlists';

/** What a failed library query was asking for, minus the search term itself. */
export type LibraryQueryContext = {
  chip: LibraryQueryChip;
  /** Any chip's sort key: the playlists chip sorts by keys the server's LibrarySort lacks. */
  sort: string;
  isSearching: boolean;
};

/**
 * A failed library load renders one generic "Something went wrong" whichever chip
 * and whichever failure produced it, so without this line the screen's primary
 * failure mode reaches production logs as nothing at all (#1704).
 */
export function useLoggedLibraryQueryFailure(
  error: Error | null,
  { chip, sort, isSearching }: LibraryQueryContext,
): void {
  useEffect(() => {
    if (error === null) {
      return;
    }
    console.warn(`[library] ${chip} query failed`, {
      sort,
      isSearching,
      ...failureLogFields(error),
    });
  }, [error, chip, sort, isSearching]);
}
