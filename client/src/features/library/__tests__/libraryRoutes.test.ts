import { featuringDetailRoute, goBackOrToLibrary } from '@features/library/domain/libraryRoutes';
import { createMemoryNavigator } from '@shared/navigation/__tests__/memoryNavigator';

describe('featuringDetailRoute', () => {
  it('routes to the discover detail screen under the discover tab', () => {
    expect(featuringDetailRoute(['(tabs)', 'discover', 'featuring'])).toBe('/discover/detail');
  });

  it('routes to the library detail screen under the library tab', () => {
    expect(featuringDetailRoute(['(tabs)', 'library', 'featuring'])).toBe('/library/detail');
  });

  it('defaults to the discover detail screen for an unknown segment', () => {
    expect(featuringDetailRoute(['(tabs)', 'search', 'featuring'])).toBe('/discover/detail');
  });
});

describe('featuringDetailRoute before the router has resolved a route', () => {
  it('uses the discover default when there are no segments yet', () => {
    expect(featuringDetailRoute([])).toBe('/discover/detail');
  });
});

function routerWithHistory(hasHistory: boolean) {
  return {
    canGoBack: () => hasHistory,
    back: jest.fn(),
    replace: jest.fn(),
  } as unknown as Parameters<typeof goBackOrToLibrary>[0];
}

describe('goBackOrToLibrary', () => {
  it('goes back when there is history to go back to', () => {
    const router = routerWithHistory(true);

    goBackOrToLibrary(router);

    expect(router.back).toHaveBeenCalledTimes(1);
    expect(router.replace).not.toHaveBeenCalled();
  });

  it('replaces with the library root when there is no history', () => {
    const router = routerWithHistory(false);

    goBackOrToLibrary(router);

    expect(router.replace).toHaveBeenCalledWith('/library');
    expect(router.back).not.toHaveBeenCalled();
  });

  it('pops the history entry on a memory navigator that has one', () => {
    const navigator = createMemoryNavigator('/discover');
    navigator.push('/library/detail');

    goBackOrToLibrary(navigator);

    expect(navigator.current()).toBe('/discover');
  });

  it('lands on the library root from a memory navigator with no history', () => {
    const navigator = createMemoryNavigator('/discover/detail');

    goBackOrToLibrary(navigator);

    expect(navigator.current()).toBe('/library');
    expect(navigator.canGoBack()).toBe(false);
  });
});
