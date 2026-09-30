import { useEffect, useRef, useState } from 'react';

import { onSignOut } from '@shared/session/signOutCleanup';

import { getSearchState } from '../searchState';
import { MAX_QUERY_LENGTH, isSearchableQuery } from '../searchLimits';

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

export function useDebouncedSearch({
  debounceMs,
}: UseDebouncedSearchOptions): UseDebouncedSearchReturn {
  const savedState = getSearchState();
  const [committedQuery, setCommittedQuery] = useState(savedState.query);
  const [inputValue, setInputValue] = useState(savedState.inputValue);
  const [isExplicitSubmit, setIsExplicitSubmit] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const clearDebounce = (): void => {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
  };

  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, []);

  useEffect(
    () =>
      onSignOut(() => {
        if (debounceRef.current) clearTimeout(debounceRef.current);
        debounceRef.current = null;
        setInputValue('');
        setCommittedQuery('');
        setIsExplicitSubmit(false);
      }),
    [],
  );

  const dropCommittedQuery = (): void => {
    setIsExplicitSubmit(false);
    setCommittedQuery('');
  };

  const onSubmit = (): void => {
    clearDebounce();
    const trimmed = inputValue.trim();
    if (!isSearchableQuery(trimmed)) {
      dropCommittedQuery();
      return;
    }
    setIsExplicitSubmit(true);
    setCommittedQuery(trimmed);
  };

  const scheduleCommit = (trimmed: string): void => {
    debounceRef.current = setTimeout(() => {
      setIsExplicitSubmit(false);
      setCommittedQuery(trimmed);
    }, debounceMs);
  };

  const onChangeText = (rawText: string): void => {
    const text = rawText.slice(0, MAX_QUERY_LENGTH);
    setInputValue(text);
    clearDebounce();
    const trimmed = text.trim();
    if (isSearchableQuery(trimmed)) scheduleCommit(trimmed);
    else dropCommittedQuery();
  };

  const onClear = (): void => {
    clearDebounce();
    setInputValue('');
    dropCommittedQuery();
  };

  const setQuery = (query: string): void => {
    clearDebounce();
    const trimmed = query.slice(0, MAX_QUERY_LENGTH).trim();
    setInputValue(trimmed);
    if (!isSearchableQuery(trimmed)) {
      dropCommittedQuery();
      return;
    }
    setIsExplicitSubmit(true);
    setCommittedQuery(trimmed);
  };

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
