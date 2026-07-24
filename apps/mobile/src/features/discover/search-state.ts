let _lastQuery = '';
let _lastInputValue = '';

export function setSearchState(query: string, inputValue: string): void {
  _lastQuery = query;
  _lastInputValue = inputValue;
}

export function getSearchState(): { query: string; inputValue: string } {
  return { query: _lastQuery, inputValue: _lastInputValue };
}
