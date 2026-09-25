import React from 'react';
import { act, render, screen, fireEvent } from '@testing-library/react-native';

import { useThemePreference } from '@shared/ui/theme/themePreference';
import { AppearanceCard } from '../ui/AppearanceCard';

describe('AppearanceCard', () => {
  afterEach(() => {
    act(() => {
      useThemePreference.setState({ scheme: 'dark' });
    });
  });

  it('shows the dark segment selected with no ADR-0008 caveat', () => {
    useThemePreference.setState({ scheme: 'dark' });
    render(<AppearanceCard />);

    expect(screen.getByTestId('settings-theme-dark').props.accessibilityState.selected).toBe(true);
    expect(screen.getByTestId('settings-theme-light').props.accessibilityState.selected).toBe(
      false,
    );
    expect(screen.queryByText('Light mode has no design pass yet (ADR-0008)')).toBeNull();
  });

  it('shows the light-mode caveat and selects the light segment on press', () => {
    render(<AppearanceCard />);

    fireEvent.press(screen.getByTestId('settings-theme-light'));

    expect(screen.getByText('Light mode has no design pass yet (ADR-0008)')).toBeTruthy();
    expect(screen.getByTestId('settings-theme-light').props.accessibilityState.selected).toBe(
      true,
    );
  });
});
