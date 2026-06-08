/**
 * search-state — module-level holder for the last search query.
 *
 * Preserves the search query across navigation (detail → back → discover).
 * Without this, navigating to detail and back would lose the search state
 * because React component state doesn't persist across unmounts.
 */

let _lastQuery = '';
let _lastInputValue = '';

export function setSearchState(query: string, inputValue: string): void {
  _lastQuery = query;
  _lastInputValue = inputValue;
}

export function getSearchState(): { query: string; inputValue: string } {
  return { query: _lastQuery, inputValue: _lastInputValue };
}

export function clearSearchState(): void {
  _lastQuery = '';
  _lastInputValue = '';
}
