import { useEffect, useRef, useState } from 'react';

import { getSearchState } from '../search-state';
import { MIN_QUERY_LENGTH, isSearchableQuery } from '../searchLimits';

type UseDebouncedSearchOptions = {
  debounceMs: number;
  minChars?: number;
};

type UseDebouncedSearchReturn = {
  inputValue: string;
  committedQuery: string;
  isExplicitSubmit: boolean;
  onChangeText: (text: string) => void;
  onSubmit: () => void;
  onClear: () => void;
  setQuery: (query: string) => void;
  setInputValue: (value: string) => void;
};

export function useDebouncedSearch({
  debounceMs,
  minChars = MIN_QUERY_LENGTH,
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

  // A keystroke just before unmount must not commit into a detached instance.
  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, []);

  const isCommittable = (trimmedQuery: string): boolean =>
    isSearchableQuery(trimmedQuery, minChars);

  // Below the commit threshold (including empty): drop the stale committed
  // query so results never outlive the text that produced them.
  const dropCommittedQuery = (): void => {
    setIsExplicitSubmit(false);
    setCommittedQuery('');
  };

  // The keyboard's return key reaches the same threshold as a keystroke: a
  // query too short to search is also too short to save into history.
  const onSubmit = (): void => {
    clearDebounce();
    const trimmed = inputValue.trim();
    if (!isCommittable(trimmed)) {
      dropCommittedQuery();
      return;
    }
    setIsExplicitSubmit(true);
    setCommittedQuery(trimmed);
  };

  const onChangeText = (text: string): void => {
    setInputValue(text);
    clearDebounce();
    const trimmed = text.trim();
    if (!isCommittable(trimmed)) {
      dropCommittedQuery();
      return;
    }
    debounceRef.current = setTimeout(() => {
      setIsExplicitSubmit(false);
      setCommittedQuery(trimmed);
    }, debounceMs);
  };

  const onClear = (): void => {
    clearDebounce();
    setInputValue('');
    dropCommittedQuery();
  };

  const setQuery = (query: string): void => {
    clearDebounce();
    setInputValue(query);
    setIsExplicitSubmit(true);
    setCommittedQuery(query);
  };

  return {
    inputValue,
    committedQuery,
    isExplicitSubmit,
    onChangeText,
    onSubmit,
    onClear,
    setQuery,
    setInputValue,
  };
}
