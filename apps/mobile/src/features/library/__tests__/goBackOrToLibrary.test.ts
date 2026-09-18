// #1689: the one owner of "leave this library screen". The two arms must stay distinct —
// a fallback that also fired back(), or a back that also replaced, would double-navigate.

import { goBackOrToLibrary } from '../goBackOrToLibrary';

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
});
