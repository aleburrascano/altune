import { renderHook } from '@testing-library/react-native';

import { CONTENT_MAX_WIDTH, WIDE_LAYOUT_MIN_WIDTH } from '@shared/ui/layout/breakpoints';
import { layoutModeFor, useLayoutMode, useWideWebLayout } from '@shared/ui/layout/useLayoutMode';

let mockWindowWidth = 390;

jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockWindowWidth, height: 800, scale: 2, fontScale: 1 }),
}));

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
    mockWindowWidth = 360;

    const { result } = renderHook(() => useLayoutMode());

    expect(result.current).toBe('compact');
  });

  it('derives wide from the current window width', () => {
    mockWindowWidth = 1440;

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
  mockWindowWidth = 390;
});

describe('useWideWebLayout', () => {
  it('stays compact on a wide native window', () => {
    mockWindowWidth = 1440;

    const { result } = renderHook(() => useWideWebLayout());

    expect(result.current).toBe(false);
  });
});
