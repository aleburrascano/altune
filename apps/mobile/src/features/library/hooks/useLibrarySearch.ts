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
  /** Predicate over arbitrary text (album/artist/playlist names). True when the query is empty. */
  matches: (text: string) => boolean;
  hasQuery: boolean;
  /** The query actually filtering right now (committed, not the raw input) — for "No results for X". */
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
      // Below the commit threshold there is no active query — clear it
      // immediately. Leaving the previous committedQuery in place (the old
      // length===1 gap) kept filtering the library by "keep on lov" while the
      // box showed a single character.
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

  const matches = useMemo(() => {
    if (!queryLower) return () => true;
    return (text: string) => text.toLowerCase().includes(queryLower);
  }, [queryLower]);

  return {
    inputValue,
    onChangeText,
    onSubmit,
    onClear,
    filter: filterFn,
    matches,
    hasQuery: committedQuery.length > 0,
    query: committedQuery,
  };
}
