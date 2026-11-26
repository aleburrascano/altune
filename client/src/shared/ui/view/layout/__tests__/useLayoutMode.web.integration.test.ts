import { act, renderHook } from '@testing-library/react-native';

import { WIDE_LAYOUT_MIN_WIDTH, useWideWebLayout } from '@shared/ui';

function setWindowWidth(width: number): void {
  Object.defineProperty(document.documentElement, 'clientWidth', {
    configurable: true,
    value: width,
  });
  act(() => {
    window.dispatchEvent(new Event('resize'));
  });
}

describe('useWideWebLayout on web', () => {
  it('is wide on a wide window', () => {
    setWindowWidth(WIDE_LAYOUT_MIN_WIDTH);

    const { result } = renderHook(() => useWideWebLayout());

    expect(result.current).toBe(true);
  });

  it('stays compact on a narrow window', () => {
    setWindowWidth(WIDE_LAYOUT_MIN_WIDTH - 1);

    const { result } = renderHook(() => useWideWebLayout());

    expect(result.current).toBe(false);
  });
});
