import type { Href, Navigator } from '../index';

export function createMemoryNavigator(initial: Href = '/'): Navigator & { current(): Href } {
  const behind: Href[] = [];
  let top: Href = initial;
  return {
    push: (href) => {
      behind.push(top);
      top = href;
    },
    replace: (href) => {
      top = href;
    },
    back: () => {
      top = behind.pop() ?? top;
    },
    canGoBack: () => behind.length > 0,
    current: () => top,
  };
}
