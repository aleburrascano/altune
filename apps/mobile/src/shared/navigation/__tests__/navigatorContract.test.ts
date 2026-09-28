import { createElement } from 'react';
import { act, renderRouter } from 'expo-router/testing-library';

import { useNavigator, type Navigator } from '../index';
import { createMemoryNavigator } from './memoryNavigator';

jest.mock('decode-uri-component', () => ({
  __esModule: true,
  default: (s: string) => {
    try {
      return decodeURIComponent(s);
    } catch {
      return s;
    }
  },
}));

function expoRouterNavigator(): Navigator {
  let captured: Navigator | undefined;
  function Probe() {
    captured = useNavigator();
    return null;
  }
  renderRouter({
    index: () => createElement(Probe),
    library: () => null,
    detail: () => null,
  });
  const navigator = captured as Navigator;
  const inAct = (fn: () => void) => () => act(fn);
  return {
    push: (href) => inAct(() => navigator.push(href))(),
    replace: (href) => inAct(() => navigator.replace(href))(),
    back: inAct(() => navigator.back()),
    canGoBack: () => navigator.canGoBack(),
  };
}

describe.each<[string, () => Navigator]>([
  ['createMemoryNavigator', () => createMemoryNavigator()],
  ['useNavigator under expo-router', expoRouterNavigator],
])('%s satisfies the Navigator contract', (_name, makeNavigator) => {
  let navigator: Navigator;

  beforeEach(() => {
    navigator = makeNavigator();
  });

  it('cannot go back at the root', () => {
    expect(navigator.canGoBack()).toBe(false);
  });

  it('can go back after a push', () => {
    navigator.push('/library');

    expect(navigator.canGoBack()).toBe(true);
  });

  it('cannot go back after pushing and returning to the root', () => {
    navigator.push('/library');
    navigator.back();

    expect(navigator.canGoBack()).toBe(false);
  });

  it('does not grow history on replace', () => {
    navigator.replace('/library');

    expect(navigator.canGoBack()).toBe(false);
  });

  it('keeps the pushed entry reachable after a replace on top of it', () => {
    navigator.push('/library');
    navigator.replace('/detail');
    navigator.back();

    expect(navigator.canGoBack()).toBe(false);
  });
});
