import { Dimensions } from 'react-native';

export const PHONE_WINDOW_WIDTH = 390;

export function setWindowWidth(width: number): void {
  const window = { width, height: 800, scale: 2, fontScale: 1 };
  Dimensions.set({ window, screen: window });
}
