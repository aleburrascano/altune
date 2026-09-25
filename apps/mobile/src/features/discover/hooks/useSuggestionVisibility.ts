import { useState, type Dispatch, type SetStateAction } from 'react';

import { isSearchableQuery } from '../searchLimits';

type SearchInput = {
  inputValue: string;
  onChangeText: (text: string) => void;
  onSubmit: () => void;
  setQuery: (query: string) => void;
};

type SuggestionVisibility = {
  isFocused: boolean;
  setIsFocused: Dispatch<SetStateAction<boolean>>;
  showSuggestions: boolean;
  onChangeText: (text: string) => void;
  onSubmit: () => void;
  onSuggestionSelect: (text: string) => void;
};

/**
 * Decides when the autocomplete dropdown shows: the input is focused, holds a
 * searchable query, has suggestions, and the user has not just submitted or
 * picked one. Typing again re-opens it.
 */
export function useSuggestionVisibility(
  search: SearchInput,
  suggestionCount: number,
): SuggestionVisibility {
  const [isFocused, setIsFocused] = useState(false);
  const [suggestionsHidden, setSuggestionsHidden] = useState(false);
  const showSuggestions =
    isFocused &&
    !suggestionsHidden &&
    isSearchableQuery(search.inputValue) &&
    suggestionCount > 0;
  return {
    isFocused,
    setIsFocused,
    showSuggestions,
    onChangeText: (text) => {
      setSuggestionsHidden(false);
      search.onChangeText(text);
    },
    onSubmit: () => {
      setSuggestionsHidden(true);
      search.onSubmit();
    },
    onSuggestionSelect: (text) => {
      setSuggestionsHidden(true);
      search.setQuery(text);
    },
  };
}
