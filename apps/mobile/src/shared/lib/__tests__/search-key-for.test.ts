import { discoveryKeys, isSearchKeyFor } from '../query-keys';

describe('isSearchKeyFor', () => {
  it('matches the key built by discoveryKeys.search for the same query', () => {
    expect(isSearchKeyFor(discoveryKeys.search('abba'), 'abba')).toBe(true);
  });

  it('matches when trailing segments are appended to the search key', () => {
    expect(isSearchKeyFor([...discoveryKeys.search('abba'), true], 'abba')).toBe(true);
  });

  it('rejects a key built for a different query', () => {
    expect(isSearchKeyFor(discoveryKeys.search('abba'), 'queen')).toBe(false);
  });
});
