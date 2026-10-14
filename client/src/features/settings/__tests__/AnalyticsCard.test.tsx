import React from 'react';
import { act, render, screen, fireEvent } from '@testing-library/react-native';

import { useAnalyticsPreference } from '@shared/device/analyticsPreference';
import { AnalyticsCard } from '@features/settings/ui/AnalyticsCard';

describe('AnalyticsCard', () => {
  afterEach(() => {
    act(() => {
      useAnalyticsPreference.setState({ shareAnalytics: true });
    });
  });

  it('shows On selected while analytics are shared', () => {
    useAnalyticsPreference.setState({ shareAnalytics: true });
    render(<AnalyticsCard />);

    expect(screen.getByTestId('settings-analytics-on').props.accessibilityState.selected).toBe(
      true,
    );
    expect(screen.getByTestId('settings-analytics-off').props.accessibilityState.selected).toBe(
      false,
    );
  });

  it('shows Off selected when the store says analytics are not shared', () => {
    useAnalyticsPreference.setState({ shareAnalytics: false });
    render(<AnalyticsCard />);

    expect(screen.getByTestId('settings-analytics-off').props.accessibilityState.selected).toBe(
      true,
    );
  });

  it('sets the store to false when Off is tapped', () => {
    render(<AnalyticsCard />);

    fireEvent.press(screen.getByTestId('settings-analytics-off'));

    expect(useAnalyticsPreference.getState().shareAnalytics).toBe(false);
  });
});
