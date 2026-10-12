import { act, renderHook } from '@testing-library/react-native';

import { WIDE_LAYOUT_MIN_WIDTH, useWideWebLayout } from '@shared/ui/layout/useLayoutMode';

function setWindowWidth(width: number): void {
  jest.spyOn(document.documentElement, 'clientWidth', 'get').mockReturnValue(width);
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
