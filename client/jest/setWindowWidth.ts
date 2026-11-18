import { act } from '@testing-library/react-native';
import { Dimensions } from 'react-native';

export const WINDOW_WIDTH = {
  smallPhone: 360,
  phone: 390,
  tablet: 800,
  justUnderWideWeb: 999,
  wideWeb: 1000,
  laptop: 1024,
  desktop: 1440,
  fullHd: 1920,
} as const;

export function setWindowWidth(width: number): void {
  const window = { width, height: 800, scale: 2, fontScale: 1 };
  act(() => {
    Dimensions.set({ window, screen: window });
  });
}
