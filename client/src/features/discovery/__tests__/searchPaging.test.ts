import {
  MAX_SEARCH_OFFSET,
  MIN_QUERY_LENGTH,
  clampQueryWords,
  isSearchableQuery,
  sliceCodePoints,
} from '@features/discovery/domain/searchPaging';

describe('isSearchableQuery', () => {
  it('rejects text whose trimmed length is below the minimum', () => {
    expect(isSearchableQuery('a'.repeat(MIN_QUERY_LENGTH - 1))).toBe(false);
    expect(isSearchableQuery(` ${'a'.repeat(MIN_QUERY_LENGTH - 1)} `)).toBe(false);
  });

  it('accepts text whose trimmed length reaches the minimum', () => {
    expect(isSearchableQuery('a'.repeat(MIN_QUERY_LENGTH))).toBe(true);
    expect(isSearchableQuery(`  ${'a'.repeat(MIN_QUERY_LENGTH)}  `)).toBe(true);
  });
});

const SERVER_WORD_CAP = 32;

const wordsOf = (count: number): string => Array.from({ length: count }, () => 'w').join(' ');

const numberedWords = (count: number): string[] =>
  Array.from({ length: count }, (_, index) => `w${index}`);

const joinWithMixedWhitespace = (words: string[]): string => {
  const separators = [' \t\n ', ' ', '　 ', '  '];
  return words.reduce(
    (text, word, index) => `${text}${separators[index % separators.length]}${word}`,
  );
};

describe('clampQueryWords', () => {
  it('leaves a query under the word cap unchanged', () => {
    expect(clampQueryWords(wordsOf(SERVER_WORD_CAP - 1))).toBe(wordsOf(SERVER_WORD_CAP - 1));
  });

  it('leaves a query at the word cap unchanged, trailing space included', () => {
    expect(clampQueryWords(wordsOf(SERVER_WORD_CAP))).toBe(wordsOf(SERVER_WORD_CAP));
    expect(clampQueryWords(`${wordsOf(SERVER_WORD_CAP)}  `)).toBe(`${wordsOf(SERVER_WORD_CAP)}  `);
  });

  it('cuts a query over the word cap after its last allowed word', () => {
    expect(clampQueryWords(wordsOf(SERVER_WORD_CAP + 1))).toBe(wordsOf(SERVER_WORD_CAP));
  });

  it('counts runs of mixed whitespace as one separator', () => {
    const spaced = joinWithMixedWhitespace(numberedWords(SERVER_WORD_CAP + 1));
    const expected = joinWithMixedWhitespace(numberedWords(SERVER_WORD_CAP));
    expect(clampQueryWords(spaced)).toBe(expected);
  });
});

describe('MAX_SEARCH_OFFSET', () => {
  it('matches the server MaxSearchOffset', () => {
    expect(MAX_SEARCH_OFFSET).toBe(200);
  });
});

describe('sliceCodePoints', () => {
  it('cuts BMP-only text at max characters', () => {
    expect(sliceCodePoints('abcdef', 3)).toBe('abc');
  });

  it('keeps an astral character whole when it ends exactly at the boundary', () => {
    expect(sliceCodePoints('ab😀cd', 3)).toBe('ab😀');
  });

  it('drops an astral character that would straddle the boundary', () => {
    expect(sliceCodePoints('ab😀😀', 3)).toBe('ab😀');
    expect(sliceCodePoints('😀😀', 1)).toBe('😀');
  });

  it('returns text shorter than max unchanged', () => {
    expect(sliceCodePoints('a😀', 10)).toBe('a😀');
  });

  it('returns an empty string for empty text', () => {
    expect(sliceCodePoints('', 5)).toBe('');
  });
});
