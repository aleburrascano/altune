export const SEARCH_PAGE_SIZE = 20;

export const MIN_QUERY_LENGTH = 2;

export const MAX_QUERY_LENGTH = 200;

export const MAX_SEARCH_PAGES = 25;

export function isSearchableQuery(text: string): boolean {
  return text.trim().length >= MIN_QUERY_LENGTH;
}
