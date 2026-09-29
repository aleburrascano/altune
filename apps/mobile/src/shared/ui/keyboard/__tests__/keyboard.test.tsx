import { Keyboard, Text } from 'react-native';
import type * as ReactNative from 'react-native';
import type * as KeyboardExports from '../keyboard';
import { fireEvent, render, screen } from '@testing-library/react-native';

type KeyboardModule = typeof KeyboardExports;

function loadWithOS(os: 'ios' | 'android'): KeyboardModule {
  let loaded!: KeyboardModule;
  jest.isolateModules(() => {
    const { Platform: isolatedPlatform } = jest.requireActual<typeof ReactNative>('react-native');
    isolatedPlatform.OS = os;
    loaded = jest.requireActual<KeyboardModule>('../keyboard');
  });
  return loaded;
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
    const { DismissKeyboardArea } = loadWithOS('ios');
    render(
      <DismissKeyboardArea testID="area">
        <Text>child</Text>
      </DismissKeyboardArea>,
    );
    fireEvent.press(screen.getByTestId('area'));
    expect(dismiss).toHaveBeenCalledTimes(1);
  });

  it('dismissKeyboard calls Keyboard.dismiss', () => {
    loadWithOS('ios').dismissKeyboard();
    expect(dismiss).toHaveBeenCalledTimes(1);
  });

  it('avoids the keyboard with padding on ios', () => {
    const m = loadWithOS('ios');
    expect([m.keyboardAvoidingBehavior, m.adjustsKeyboardInsets]).toEqual(['padding', true]);
  });

  it('leaves keyboard avoidance to the system on android', () => {
    const m = loadWithOS('android');
    expect([m.keyboardAvoidingBehavior, m.adjustsKeyboardInsets]).toEqual([undefined, false]);
  });
});
