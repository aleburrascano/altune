import { useRef, useState } from 'react';

import { getSearchState } from '@shared/lib/search-state';

type UseDebouncedSearchOptions = {
  debounceMs: number;
  minChars: number;
};

type UseDebouncedSearchReturn = {
  inputValue: string;
  committedQuery: string;
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
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const clearDebounce = (): void => {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
  };

  const onSubmit = (): void => {
    clearDebounce();
    setCommittedQuery(inputValue.trim());
  };

  const onChangeText = (text: string): void => {
    setInputValue(text);
    clearDebounce();
    const trimmed = text.trim();
    if (trimmed.length === 0) {
      setCommittedQuery('');
    } else if (trimmed.length >= minChars) {
      debounceRef.current = setTimeout(() => {
        setCommittedQuery(trimmed);
      }, debounceMs);
    }
  };

  const onClear = (): void => {
    clearDebounce();
    setInputValue('');
    setCommittedQuery('');
  };

  const setQuery = (query: string): void => {
    setInputValue(query);
    setCommittedQuery(query);
  };

  return {
    inputValue,
    committedQuery,
    onChangeText,
    onSubmit,
    onClear,
    setQuery,
    setInputValue,
  };
}
