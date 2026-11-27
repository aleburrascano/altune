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

function clampQuery(text: string): string {
  return clampQueryWords(sliceCodePoints(text, MAX_QUERY_LENGTH));
}

export function useDebouncedSearch({
  debounceMs,
  onExplicitCommit,
}: UseDebouncedSearchOptions): UseDebouncedSearchReturn {
  const savedState = getSearchState();
  const [committedQuery, setCommittedQuery] = useState(savedState.query);
  const [inputValue, setInputValue] = useState(savedState.inputValue);
  const [isExplicitSubmit, setIsExplicitSubmit] = useState(false);
  const debounce = useDebouncedCall(debounceMs);
  const cancelDebounce = debounce.cancel;

  useEffect(() => {
    setSearchState(committedQuery, inputValue);
  }, [committedQuery, inputValue]);

  useEffect(
    () =>
      onSignOut(() => {
        cancelDebounce();
        setInputValue('');
        setCommittedQuery('');
        setIsExplicitSubmit(false);
      }),
    [cancelDebounce],
  );

  function dropCommittedQuery(): void {
    setIsExplicitSubmit(false);
    setCommittedQuery('');
  }

  function commitExplicit(query: string): void {
    if (!isSearchableQuery(query)) {
      dropCommittedQuery();
      return;
    }
    const isNewCommit = !isExplicitSubmit || query !== committedQuery;
    setIsExplicitSubmit(true);
    setCommittedQuery(query);
    if (isNewCommit) onExplicitCommit?.(query);
  }

  function scheduleCommit(query: string): void {
    debounce.schedule(() => {
      setIsExplicitSubmit(false);
      setCommittedQuery(query);
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
