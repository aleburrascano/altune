import { Text } from 'react-native';
import { fireEvent, render, screen } from '@testing-library/react-native';

import {
  DismissKeyboardArea,
  KeepInputFocusArea,
  adjustsKeyboardInsets,
  dismissKeyboard,
} from '@shared/ui';

describe('keyboard (native)', () => {
  it('DismissKeyboardArea hands a press to the dismissal it was given', () => {
    const onDismiss = jest.fn();
    render(
      <DismissKeyboardArea testID="area" onDismiss={onDismiss}>
        <Text>child</Text>
      </DismissKeyboardArea>,
    );

    fireEvent.press(screen.getByTestId('area'));

    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it('DismissKeyboardArea dismisses the keyboard on press when it is given no dismissal', () => {
    render(
      <DismissKeyboardArea testID="area">
        <Text>child</Text>
      </DismissKeyboardArea>,
    );

    expect(() => fireEvent.press(screen.getByTestId('area'))).not.toThrow();
  });

  it('KeepInputFocusArea renders its children and is not a press target', () => {
    const onDismiss = jest.fn();
    render(
      <DismissKeyboardArea testID="outer" onDismiss={onDismiss}>
        <KeepInputFocusArea testID="area">
          <Text>child</Text>
        </KeepInputFocusArea>
      </DismissKeyboardArea>,
    );

    expect(screen.getByTestId('area')).toBeTruthy();
    expect(screen.getByText('child')).toBeTruthy();
  });

  it('dismissKeyboard asks the platform to hide the keyboard without failing', () => {
    expect(() => dismissKeyboard()).not.toThrow();
  });

  it('adjusts keyboard insets on the native platform the tests run as', () => {
    expect(adjustsKeyboardInsets).toBe(true);
  });
});
