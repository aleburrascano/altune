import { MIN_QUERY_LENGTH, isSearchableQuery } from '../searchLimits';

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
