import { useEffect } from 'react';
import type { LibrarySort } from '@features/library/core/libraryGroups';
import { failureLogFields } from '@features/library/core/errors';
import { useLibraryPort } from '@features/library/view/LibraryProvider';
import type { LibraryChip } from '@features/library/core/libraryView';

type LibraryQueryChip = LibraryChip;

type LibraryQueryContext = {
  chip: LibraryQueryChip;
  sort?: LibrarySort;
  isSearching: boolean;
};

export function useLoggedLibraryQueryFailure(
  error: Error | null,
  { chip, sort, isSearching }: LibraryQueryContext,
): void {
  const { warn } = useLibraryPort();
  useEffect(() => {
    if (error === null) {
      return;
    }
    warn(`[library] ${chip} query failed`, {
      sort,
      isSearching,
      ...failureLogFields(error),
    });
  }, [warn, error, chip, sort, isSearching]);
}
