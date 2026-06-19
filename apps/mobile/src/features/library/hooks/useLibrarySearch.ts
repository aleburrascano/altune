import { useMemo, useRef, useState } from 'react';

import type { TrackResponse } from '@shared/api-client/types';

const DEBOUNCE_MS = 300;
const MIN_CHARS = 2;

interface UseLibrarySearchReturn {
  inputValue: string;
  onChangeText: (text: string) => void;
  onSubmit: () => void;
  onClear: () => void;
  filter: (tracks: readonly TrackResponse[]) => readonly TrackResponse[];
  hasQuery: boolean;
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
    if (trimmed.length === 0) {
      setCommittedQuery('');
    } else if (trimmed.length >= MIN_CHARS) {
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

  const queryLower = committedQuery.toLowerCase();

  const filterFn = useMemo(() => {
    if (!queryLower) return (tracks: readonly TrackResponse[]) => tracks;
    return (tracks: readonly TrackResponse[]) =>
      tracks.filter((t) =>
        t.title.toLowerCase().includes(queryLower)
        || t.artist.toLowerCase().includes(queryLower)
        || (t.album != null && t.album.toLowerCase().includes(queryLower)),
      );
  }, [queryLower]);

  return {
    inputValue,
    onChangeText,
    onSubmit,
    onClear,
    filter: filterFn,
    hasQuery: committedQuery.length > 0,
  };
}
