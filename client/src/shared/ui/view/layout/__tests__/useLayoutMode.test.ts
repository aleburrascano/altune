import { renderHook } from '@testing-library/react-native';
import { setWindowWidth, WINDOW_WIDTH } from '@testing/setWindowWidth';

import {
  CONTENT_MAX_WIDTH,
  WIDE_LAYOUT_MIN_WIDTH,
  useLayoutMode,
  useWideWebLayout,
  type LayoutMode,
} from '@shared/ui';
function layoutModeFor(width: number): LayoutMode {
  setWindowWidth(width);
  return renderHook(() => useLayoutMode()).result.current;
}

describe('layoutModeFor', () => {
  it('stays compact just below the wide breakpoint', () => {
    expect(layoutModeFor(WIDE_LAYOUT_MIN_WIDTH - 1)).toBe('compact');
  });

  it('switches to wide at the breakpoint', () => {
    expect(layoutModeFor(WIDE_LAYOUT_MIN_WIDTH)).toBe('wide');
  });
});

describe('useLayoutMode', () => {
  it('derives compact from the current window width', () => {
    setWindowWidth(WINDOW_WIDTH.smallPhone);

    const { result } = renderHook(() => useLayoutMode());

    expect(result.current).toBe('compact');
  });

  it('derives wide from the current window width', () => {
    setWindowWidth(WINDOW_WIDTH.desktop);

    const { result } = renderHook(() => useLayoutMode());

    expect(result.current).toBe('wide');
  });
});

describe('CONTENT_MAX_WIDTH', () => {
  it('caps the wide-mode content column at 1200', () => {
    expect(CONTENT_MAX_WIDTH).toBe(1200);
  });
});

afterEach(() => {
  setWindowWidth(WINDOW_WIDTH.phone);
});

describe('useWideWebLayout', () => {
  it('stays compact on a wide native window', () => {
    setWindowWidth(WINDOW_WIDTH.desktop);

    const { result } = renderHook(() => useWideWebLayout());

    expect(result.current).toBe(false);
  });
});
