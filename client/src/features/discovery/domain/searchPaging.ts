import type { InfiniteData } from '@tanstack/react-query';

import type { DiscoverySearchResponse } from '@shared/api-client/discovery';

export type SearchPageParam = { offset: number; searchId: string | undefined };

export type SearchPages = InfiniteData<DiscoverySearchResponse, SearchPageParam>;

export const SEARCH_PAGE_SIZE = 20;

export const MIN_QUERY_LENGTH = 2;

export const MAX_QUERY_LENGTH = 200;

const MAX_QUERY_WORDS = 32;

export const MAX_SEARCH_PAGES = 25;

export const MAX_SEARCH_OFFSET = 200;

export function isSearchableQuery(text: string): boolean {
  return text.trim().length >= MIN_QUERY_LENGTH;
}

const WORD_PATTERN =
  /[^\t\n\v\f\r \u0085\u00A0\u1680\u2000-\u200A\u2028\u2029\u202F\u205F\u3000]+/g;

export function clampQueryWords(text: string): string {
  const words = [...text.matchAll(WORD_PATTERN)];
  if (words.length <= MAX_QUERY_WORDS) return text;
  const allowedWords = words.slice(0, MAX_QUERY_WORDS);
  const allowedEnd = Math.max(...allowedWords.map((word) => word.index + word[0].length));
  return text.slice(0, allowedEnd);
}

export function sliceCodePoints(text: string, max: number): string {
  if (text.length <= max) return text;
  return Array.from(text).slice(0, max).join('');
}
