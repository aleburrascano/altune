import { Dimensions } from 'react-native';

export const WINDOW_WIDTH = {
  smallPhone: 360,
  phone: 390,
  tablet: 720,
  belowWideWeb: 900,
  justUnderWideWeb: 999,
  wideWeb: 1000,
  desktop: 1440,
} as const;

export function setWindowWidth(width: number): void {
  const window = { width, height: 800, scale: 2, fontScale: 1 };
  Dimensions.set({ window, screen: window });
}
