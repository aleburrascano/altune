// Probe (#2819): the pill shell TrackSavePill and SaveAllPill now share. Pins
// what a caller hands it reaching the pressable: label, testID, a11y state,
// children, and the press guard.

import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react-native';
import { Text } from 'react-native';

import { SavePillShell, type SavePillShellProps } from '../ui/SavePillShell';

function renderShell(overrides: Partial<SavePillShellProps> = {}): jest.Mock {
  const onPress = jest.fn();
  render(
    <SavePillShell
      onPress={onPress}
      disabled={false}
      interactive
      accessibilityLabel="Save everything"
      testID="shell"
      {...overrides}
    >
      <Text>Save 4</Text>
    </SavePillShell>,
  );
  return onPress;
}

describe('SavePillShell', () => {
  it('exposes the caller label, testID and content', () => {
    renderShell();

    expect(screen.getByLabelText('Save everything')).toBeTruthy();
    expect(screen.getByTestId('shell')).toBeTruthy();
    expect(screen.getByText('Save 4')).toBeTruthy();
  });

  it('announces exactly the accessibility state the caller passes', () => {
    renderShell({ disabled: true, accessibilityState: { disabled: true, busy: true } });

    const state = screen.getByTestId('shell').props.accessibilityState;
    expect(state?.disabled).toBe(true);
    expect(state?.busy).toBe(true);
  });

  it('calls onPress when tapped while enabled and interactive', () => {
    const onPress = renderShell();

    fireEvent.press(screen.getByTestId('shell'));

    expect(onPress).toHaveBeenCalledTimes(1);
  });

  it('ignores taps while disabled', () => {
    const onPress = renderShell({ disabled: true, accessibilityState: { disabled: true } });

    fireEvent.press(screen.getByTestId('shell'));

    expect(onPress).not.toHaveBeenCalled();
  });
});
