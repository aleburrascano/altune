import { onSignOut } from '@shared/session/signOutCleanup';

let _lastQuery = '';
let _lastInputValue = '';

export function setSearchState(query: string, inputValue: string): void {
  _lastQuery = query;
  _lastInputValue = inputValue;
}

export function getSearchState(): { query: string; inputValue: string } {
  return { query: _lastQuery, inputValue: _lastInputValue };
}

export function resetSearchState(): void {
  _lastQuery = '';
  _lastInputValue = '';
}

// Process-lifetime state: without this, the next account to sign in would be
// seeded with (and immediately search for) the previous account's query.
onSignOut(resetSearchState);
