import { featuringDetailRoute } from '../featuringDetailRoute';

describe('featuringDetailRoute', () => {
  it('routes to the discover detail screen under the discover tab', () => {
    expect(featuringDetailRoute(['(tabs)', 'discover', 'featuring'])).toBe('/discover/detail');
  });

  it('routes to the library detail screen under the library tab', () => {
    expect(featuringDetailRoute(['(tabs)', 'library', 'featuring'])).toBe('/library/detail');
  });

  it('defaults to the library detail screen for an unknown segment', () => {
    expect(featuringDetailRoute(['(tabs)', 'search', 'featuring'])).toBe('/library/detail');
  });
});

describe('featuringDetailRoute before the router has resolved a route', () => {
  it('keeps the library default when there are no segments yet', () => {
    expect(featuringDetailRoute([])).toBe('/library/detail');
  });
});
