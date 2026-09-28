import { useEffect } from 'react';

import type { LibrarySort } from '@shared/api-client/library';

import { failureLogFields } from '../failureLogFields';
import type { LibraryChip } from '../activeView';
import type { SortKey } from '../sort';

export type LibraryQueryChip = LibraryChip;

export type LibraryQueryContext = {
  chip: LibraryQueryChip;
  sort?: LibrarySort | SortKey;
  isSearching: boolean;
};

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
