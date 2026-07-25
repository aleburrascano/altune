import { useRef, useState } from 'react';

const DEBOUNCE_MS = 300;
const MIN_CHARS = 2;

interface UseLibrarySearchReturn {
  inputValue: string;
  onChangeText: (text: string) => void;
  onSubmit: () => void;
  onClear: () => void;
  hasQuery: boolean;
  query: string;
}

export function useLibrarySearch(): UseLibrarySearchReturn {
  const [inputValue, setInputValue] = useState('');
  const [committedQuery, setCommittedQuery] = useState('');
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const clearDebounce = (): void => {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
  };

  const onChangeText = (text: string): void => {
    setInputValue(text);
    clearDebounce();
    const trimmed = text.trim();
    if (trimmed.length < MIN_CHARS) {
      setCommittedQuery('');
    } else {
      debounceRef.current = setTimeout(() => {
        setCommittedQuery(trimmed);
      }, DEBOUNCE_MS);
    }
  };

  const onSubmit = (): void => {
    clearDebounce();
    setCommittedQuery(inputValue.trim());
  };

  const onClear = (): void => {
    clearDebounce();
    setInputValue('');
    setCommittedQuery('');
  };

  return {
    inputValue,
    onChangeText,
    onSubmit,
    onClear,
    hasQuery: committedQuery.length > 0,
    query: committedQuery,
  };
}
