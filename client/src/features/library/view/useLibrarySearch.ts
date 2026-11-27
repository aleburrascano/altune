import { useState } from 'react';

import { useDebouncedCall } from '@shared/concurrency';

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
  const debounce = useDebouncedCall(DEBOUNCE_MS);

  const onChangeText = (text: string): void => {
    setInputValue(text);
    const committable = committableOf(text);
    if (committable === '') {
      debounce.cancel();
      setCommittedQuery('');
    } else {
      debounce.schedule(() => {
        setCommittedQuery(committable);
      });
    }
  };

  const onSubmit = (): void => {
    debounce.cancel();
    setCommittedQuery(committableOf(inputValue));
  };

  const onClear = (): void => {
    debounce.cancel();
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
