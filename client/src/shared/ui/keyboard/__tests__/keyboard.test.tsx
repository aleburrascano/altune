import { Keyboard, Text } from 'react-native';
import type * as KeyboardExports from '@shared/ui/keyboard/keyboard';
import { fireEvent, render, screen } from '@testing-library/react-native';

type KeyboardModule = typeof KeyboardExports;

function loadIos(): KeyboardModule {
  return jest.requireActual<KeyboardModule>('@shared/ui/keyboard/keyboard.ios.tsx');
}

function loadDefault(): KeyboardModule {
  return jest.requireActual<KeyboardModule>('@shared/ui/keyboard/keyboard.tsx');
}

describe('keyboard (native)', () => {
  let dismiss: jest.SpyInstance;

  beforeEach(() => {
    dismiss = jest.spyOn(Keyboard, 'dismiss').mockImplementation(() => undefined);
  });

  afterEach(() => {
    dismiss.mockRestore();
  });

  it('DismissKeyboardArea dismisses the keyboard when pressed', () => {
    const { DismissKeyboardArea } = loadIos();
    render(
      <DismissKeyboardArea testID="area">
        <Text>child</Text>
      </DismissKeyboardArea>,
    );
    fireEvent.press(screen.getByTestId('area'));
    expect(dismiss).toHaveBeenCalledTimes(1);
  });

  it('KeepInputFocusArea renders its children and does not dismiss the keyboard', () => {
    const { KeepInputFocusArea } = loadIos();
    render(
      <KeepInputFocusArea testID="area">
        <Text>child</Text>
      </KeepInputFocusArea>,
    );
    fireEvent.press(screen.getByText('child'));
    expect(screen.getByTestId('area')).toBeTruthy();
    expect(dismiss).not.toHaveBeenCalled();
  });

  it('dismissKeyboard calls Keyboard.dismiss', () => {
    loadIos().dismissKeyboard();
    expect(dismiss).toHaveBeenCalledTimes(1);
  });

  it('avoids the keyboard with padding on ios', () => {
    const m = loadIos();
    expect([m.keyboardAvoidingBehavior, m.adjustsKeyboardInsets]).toEqual(['padding', true]);
  });

  it('leaves keyboard avoidance to the system on android', () => {
    const m = loadDefault();
    expect([m.keyboardAvoidingBehavior, m.adjustsKeyboardInsets]).toEqual([undefined, false]);
  });
});
