import { Keyboard, Text } from 'react-native';
import { fireEvent, render, screen } from '@testing-library/react-native';

import {
  DismissKeyboardArea,
  adjustsKeyboardInsets,
  dismissKeyboard,
  keyboardAvoidingBehavior,
} from '../keyboard.web';

describe('keyboard (web)', () => {
  let dismiss: jest.SpyInstance;

  beforeEach(() => {
    dismiss = jest.spyOn(Keyboard, 'dismiss').mockImplementation(() => undefined);
  });

  afterEach(() => {
    dismiss.mockRestore();
  });

  it('DismissKeyboardArea never dismisses the keyboard when a child is pressed', () => {
    render(
      <DismissKeyboardArea testID="area">
        <Text>child</Text>
      </DismissKeyboardArea>,
    );
    fireEvent.press(screen.getByText('child'));
    fireEvent.press(screen.getByTestId('area'));
    expect(dismiss).not.toHaveBeenCalled();
  });

  it('dismissKeyboard does nothing', () => {
    dismissKeyboard();
    expect(dismiss).not.toHaveBeenCalled();
  });

  it('has no keyboard avoidance', () => {
    expect([keyboardAvoidingBehavior, adjustsKeyboardInsets]).toEqual([undefined, false]);
  });
});
