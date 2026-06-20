import { useRef, useState } from 'react';

import { getSearchState } from '../search-state';

type UseDebouncedSearchOptions = {
  debounceMs: number;
  minChars: number;
};

type UseDebouncedSearchReturn = {
  inputValue: string;
  committedQuery: string;
  /** True when the commit came from Enter key or history tap, not auto-debounce. */
  isExplicitSubmit: boolean;
  onChangeText: (text: string) => void;
  onSubmit: () => void;
  onClear: () => void;
  setQuery: (query: string) => void;
  setInputValue: (value: string) => void;
};

export function useDebouncedSearch({
  debounceMs,
  minChars,
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

  const onSubmit = (): void => {
    clearDebounce();
    setIsExplicitSubmit(true);
    setCommittedQuery(inputValue.trim());
  };

  const onChangeText = (text: string): void => {
    setInputValue(text);
    clearDebounce();
    const trimmed = text.trim();
    if (trimmed.length === 0) {
      setIsExplicitSubmit(false);
      setCommittedQuery('');
    } else if (trimmed.length >= minChars) {
      debounceRef.current = setTimeout(() => {
        setIsExplicitSubmit(false);
        setCommittedQuery(trimmed);
      }, debounceMs);
    }
  };

  const onClear = (): void => {
    clearDebounce();
    setInputValue('');
    setIsExplicitSubmit(false);
    setCommittedQuery('');
  };

  const setQuery = (query: string): void => {
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
