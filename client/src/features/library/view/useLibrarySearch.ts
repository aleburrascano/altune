import { useEffect, useRef, useState } from 'react';

const DEBOUNCE_MS = 300;
const MIN_CHARS = 2;

function committableOf(text: string): string {
  const trimmed = text.trim();
  return trimmed.length < MIN_CHARS ? '' : trimmed;
}

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

  useEffect(() => () => clearTimeout(debounceRef.current ?? undefined), []);

  const clearDebounce = (): void => {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
  };

  const onChangeText = (text: string): void => {
    setInputValue(text);
    clearDebounce();
    const committable = committableOf(text);
    if (committable === '') {
      setCommittedQuery('');
    } else {
      debounceRef.current = setTimeout(() => {
        setCommittedQuery(committable);
      }, DEBOUNCE_MS);
    }
  };

  const onSubmit = (): void => {
    clearDebounce();
    setCommittedQuery(committableOf(inputValue));
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
