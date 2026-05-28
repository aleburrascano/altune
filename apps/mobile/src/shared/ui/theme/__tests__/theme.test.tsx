import { renderHook } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { confidenceColor } from '../confidenceColor';
import { darkTheme } from '../darkTheme';
import { lightTheme } from '../lightTheme';
import { ThemeProvider } from '../ThemeProvider';
import { useTheme } from '../useTheme';

describe('useTheme', () => {
  it('falls back to darkTheme when no ThemeProvider is mounted', () => {
    // The shipped auth tests render screens bare (no provider). The fallback
    // keeps them green while still letting a provider drive light mode later.
    const { result } = renderHook(() => useTheme());
    expect(result.current).toBe(darkTheme);
  });

  it('returns the provider theme when wrapped', () => {
    const wrapper = ({ children }: { children: ReactNode }) => (
      <ThemeProvider scheme="light">{children}</ThemeProvider>
    );
    const { result } = renderHook(() => useTheme(), { wrapper });
    expect(result.current).toBe(lightTheme);
  });
});

describe('confidenceColor', () => {
  it('maps each confidence level to its theme color', () => {
    expect(confidenceColor(darkTheme, 'high')).toBe(darkTheme.color.confHigh);
    expect(confidenceColor(darkTheme, 'medium')).toBe(darkTheme.color.confMed);
    expect(confidenceColor(darkTheme, 'low')).toBe(darkTheme.color.confLow);
  });

  it('uses the active theme, not a hardcoded palette', () => {
    expect(confidenceColor(lightTheme, 'high')).toBe(lightTheme.color.confHigh);
  });
});
