import { tabRootFromSegments } from '../navigator';

describe('tabRootFromSegments', () => {
  it('returns library for a library segment', () => {
    expect(tabRootFromSegments(['(tabs)', 'library', 'detail'])).toBe('library');
  });

  it('returns discover for a discover segment', () => {
    expect(tabRootFromSegments(['(tabs)', 'discover'])).toBe('discover');
  });

  it('falls back to discover with no segments', () => {
    expect(tabRootFromSegments([])).toBe('discover');
  });
});
