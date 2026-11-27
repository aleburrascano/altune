import { useEffect, useState } from 'react';

import { useDebouncedCall } from '@shared/concurrency';
import { onSignOut } from '@shared/session';

import { getSearchState, setSearchState } from '@features/discovery/core/searchState';
import {
  MAX_QUERY_LENGTH,
  clampQueryWords,
  isSearchableQuery,
  normalizeQuery,
  sliceCodePoints,
} from '@features/discovery/core/searchPaging';

type UseDebouncedSearchOptions = {
  debounceMs: number;
  onExplicitCommit?: (query: string) => void;
};

type UseDebouncedSearchReturn = {
  inputValue: string;
  committedQuery: string;
  isExplicitSubmit: boolean;
  onChangeText: (text: string) => void;
  onSubmit: () => void;
  onClear: () => void;
  setQuery: (query: string) => void;
};

type Commit = { query: string; isExplicit: boolean };

const NO_COMMIT: Commit = { query: '', isExplicit: false };

function clampQuery(text: string): string {
  return clampQueryWords(sliceCodePoints(text, MAX_QUERY_LENGTH));
}

export function useDebouncedSearch({
  debounceMs,
  onExplicitCommit,
}: UseDebouncedSearchOptions): UseDebouncedSearchReturn {
  const savedState = getSearchState();
  const [commit, setCommit] = useState<Commit>({ query: savedState.query, isExplicit: false });
  const [inputValue, setInputValue] = useState(savedState.inputValue);
  const debounce = useDebouncedCall(debounceMs);
  const cancelDebounce = debounce.cancel;
  const { query: committedQuery, isExplicit: isExplicitSubmit } = commit;

  useEffect(() => {
    setSearchState(committedQuery, inputValue);
  }, [committedQuery, inputValue]);

  useEffect(
    () =>
      onSignOut(() => {
        cancelDebounce();
        setInputValue('');
        setCommit(NO_COMMIT);
      }),
    [cancelDebounce],
  );

  function dropCommittedQuery(): void {
    setCommit(NO_COMMIT);
  }

  function commitExplicit(query: string): void {
    if (!isSearchableQuery(query)) {
      dropCommittedQuery();
      return;
    }
    const isNewCommit = !isExplicitSubmit || query !== committedQuery;
    setCommit({ query, isExplicit: true });
    if (isNewCommit) onExplicitCommit?.(query);
  }

  function scheduleCommit(query: string): void {
    debounce.schedule(() => {
      setCommit({ query, isExplicit: false });
    });
  }

  function onSubmit(): void {
    debounce.cancel();
    commitExplicit(normalizeQuery(inputValue));
  }

  function onChangeText(rawText: string): void {
    const text = clampQuery(rawText);
    setInputValue(text);
    debounce.cancel();
    const query = normalizeQuery(text);
    if (!isSearchableQuery(query)) dropCommittedQuery();
    else if (query !== committedQuery) scheduleCommit(query);
  }

  function onClear(): void {
    debounce.cancel();
    setInputValue('');
    dropCommittedQuery();
  }

  function setQuery(rawQuery: string): void {
    debounce.cancel();
    const query = normalizeQuery(clampQuery(rawQuery));
    setInputValue(query);
    commitExplicit(query);
  }

  return {
    inputValue,
    committedQuery,
    isExplicitSubmit,
    onChangeText,
    onSubmit,
    onClear,
    setQuery,
  };
}
