import { createSearchFocus } from '@shared/ui';

const FOCUS_REQUEST_TTL_MS = 5_000;
const DISCOVER_ROUTE = '/discover';

type RecordingNavigator = { push: (href: string) => void; pushes: string[] };

function createMemoryNavigator(): RecordingNavigator {
  const pushes: string[] = [];
  return { pushes, push: (href) => void pushes.push(href) };
}

describe('search focus', () => {
  it('focuses the registered search immediately and navigates nowhere', () => {
    const searchFocus = createSearchFocus();
    const navigator = createMemoryNavigator();
    const focus = jest.fn();
    searchFocus.register(focus);

    searchFocus.requestFocus(navigator);

    expect(focus).toHaveBeenCalledTimes(1);
    expect(navigator.pushes).toEqual([]);
  });

  it('navigates to Discover, then focuses once the search registers', () => {
    const searchFocus = createSearchFocus();
    const navigator = createMemoryNavigator();

    searchFocus.requestFocus(navigator);
    const focus = jest.fn();
    searchFocus.register(focus);

    expect(navigator.pushes).toEqual([DISCOVER_ROUTE]);
    expect(focus).toHaveBeenCalledTimes(1);
  });

  it('navigates again once the search has unregistered', () => {
    const searchFocus = createSearchFocus();
    const navigator = createMemoryNavigator();
    const focus = jest.fn();
    searchFocus.register(focus)();

    searchFocus.requestFocus(navigator);

    expect(focus).not.toHaveBeenCalled();
    expect(navigator.pushes).toEqual([DISCOVER_ROUTE]);
  });

  it('keeps a registration that a newer one replaced when the older one unregisters', () => {
    const searchFocus = createSearchFocus();
    const navigator = createMemoryNavigator();
    const older = jest.fn();
    const newer = jest.fn();
    const unregisterOlder = searchFocus.register(older);
    searchFocus.register(newer);
    unregisterOlder();

    searchFocus.requestFocus(navigator);

    expect(newer).toHaveBeenCalledTimes(1);
    expect(older).not.toHaveBeenCalled();
  });

  it('focuses only the first search that registers after navigating', () => {
    const searchFocus = createSearchFocus();
    searchFocus.requestFocus(createMemoryNavigator());
    const first = jest.fn();
    searchFocus.register(first)();
    const second = jest.fn();
    searchFocus.register(second);

    expect(first).toHaveBeenCalledTimes(1);
    expect(second).not.toHaveBeenCalled();
  });

  it('does not push again while a navigation to Discover is already pending', () => {
    const searchFocus = createSearchFocus();
    const navigator = createMemoryNavigator();

    searchFocus.requestFocus(navigator);
    searchFocus.requestFocus(navigator);

    expect(navigator.pushes).toEqual([DISCOVER_ROUTE]);
  });

  it('clears the pending request when the navigation throws, so it cannot steal a later focus', () => {
    const searchFocus = createSearchFocus();
    const failing = {
      push: () => {
        throw new Error('navigation failed');
      },
    };

    searchFocus.requestFocus(failing);
    const focus = jest.fn();
    searchFocus.register(focus);

    expect(focus).not.toHaveBeenCalled();
  });

  it('lets a stale request expire so it does not fire on a later unrelated focus', () => {
    jest.useFakeTimers();
    try {
      const searchFocus = createSearchFocus();
      searchFocus.requestFocus(createMemoryNavigator());

      jest.advanceTimersByTime(FOCUS_REQUEST_TTL_MS);
      const focus = jest.fn();
      searchFocus.register(focus);

      expect(focus).not.toHaveBeenCalled();
    } finally {
      jest.useRealTimers();
    }
  });
});
