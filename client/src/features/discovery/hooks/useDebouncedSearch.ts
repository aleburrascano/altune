import { useEffect, useRef, useState, type MutableRefObject } from 'react';

import { onSignOut } from '@shared/session/signOutCleanup';

import { getSearchState, setSearchState } from '@features/discovery/domain/searchState';
import {
  MAX_QUERY_LENGTH,
  clampQueryWords,
  isSearchableQuery,
  normalizeQuery,
  sliceCodePoints,
} from '@features/discovery/domain/searchPaging';

type UseDebouncedSearchOptions = {
  debounceMs: number;
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

type DebounceTimer = MutableRefObject<ReturnType<typeof setTimeout> | null>;

function clearTimer(timer: DebounceTimer): void {
  if (timer.current) clearTimeout(timer.current);
  timer.current = null;
}

function clampQuery(text: string): string {
  return clampQueryWords(sliceCodePoints(text, MAX_QUERY_LENGTH));
}

export function useDebouncedSearch({
  debounceMs,
}: UseDebouncedSearchOptions): UseDebouncedSearchReturn {
  const savedState = getSearchState();
  const [committedQuery, setCommittedQuery] = useState(savedState.query);
  const [inputValue, setInputValue] = useState(savedState.inputValue);
  const [isExplicitSubmit, setIsExplicitSubmit] = useState(false);
  const debounceRef: DebounceTimer = useRef(null);

  useEffect(() => {
    setSearchState(committedQuery, inputValue);
  }, [committedQuery, inputValue]);

  useEffect(() => () => clearTimer(debounceRef), []);

  useEffect(
    () =>
      onSignOut(() => {
        clearTimer(debounceRef);
        setInputValue('');
        setCommittedQuery('');
        setIsExplicitSubmit(false);
      }),
    [],
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
    setIsExplicitSubmit(true);
    setCommittedQuery(query);
  }

  function scheduleCommit(query: string): void {
    debounceRef.current = setTimeout(() => {
      setIsExplicitSubmit(false);
      setCommittedQuery(query);
    }, debounceMs);
  }

  function onSubmit(): void {
    clearTimer(debounceRef);
    commitExplicit(normalizeQuery(inputValue));
  }

  function onChangeText(rawText: string): void {
    const text = clampQuery(rawText);
    setInputValue(text);
    clearTimer(debounceRef);
    const query = normalizeQuery(text);
    if (!isSearchableQuery(query)) dropCommittedQuery();
    else if (query !== committedQuery) scheduleCommit(query);
  }

  function onClear(): void {
    clearTimer(debounceRef);
    setInputValue('');
    dropCommittedQuery();
  }

  function setQuery(rawQuery: string): void {
    clearTimer(debounceRef);
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
