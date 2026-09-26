import { renderHook } from '@testing-library/react-native';
import { Platform } from 'react-native';

import { useWideWebLayout } from '../useWideWebLayout';

let mockWindowWidth = 390;

jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockWindowWidth, height: 800, scale: 2, fontScale: 1 }),
}));

const NATIVE_OS = Platform.OS;

afterEach(() => {
  Platform.OS = NATIVE_OS;
  mockWindowWidth = 390;
});

describe('useWideWebLayout', () => {
  it('is wide on a wide web window', () => {
    Platform.OS = 'web';
    mockWindowWidth = 1440;

    const { result } = renderHook(() => useWideWebLayout());

    expect(result.current).toBe(true);
  });

  it('stays compact on a narrow web window', () => {
    Platform.OS = 'web';
    mockWindowWidth = 390;

    const { result } = renderHook(() => useWideWebLayout());

    expect(result.current).toBe(false);
  });

  it('stays compact on a wide native window', () => {
    Platform.OS = 'ios';
    mockWindowWidth = 1440;

    const { result } = renderHook(() => useWideWebLayout());

    expect(result.current).toBe(false);
  });
});
