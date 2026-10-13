import { StyleSheet } from 'react-native';
import { render, screen } from '@testing-library/react-native';
import type * as KeyboardModuleExports from '@shared/ui/keyboard/keyboard';
import type * as ModalFrameExports from '@shared/ui/primitives/ModalFrame';

type KeyboardExports = typeof KeyboardModuleExports;

const sharedReact = jest.requireActual('react');
const sharedReactNative = jest.requireActual('react-native');

function keyboardAvoidingStyleOn(os: 'ios' | 'android'): Record<string, unknown> {
  let loaded!: typeof ModalFrameExports;
  jest.isolateModules(() => {
    jest.doMock('react', () => sharedReact);
    jest.doMock('react-native', () => sharedReactNative);
    sharedReactNative.Platform.OS = os;
    if (os === 'android') {
      const platformKeyboard = jest.requireActual<KeyboardExports>('@shared/ui/keyboard/keyboard');
      const androidKeyboard = jest.requireActual<KeyboardExports>(
        '@shared/ui/keyboard/keyboard.tsx',
      );
      jest.replaceProperty(
        platformKeyboard,
        'keyboardAvoidingBehavior',
        androidKeyboard.keyboardAvoidingBehavior,
      );
    }
    loaded = jest.requireActual<typeof ModalFrameExports>('@shared/ui/primitives/ModalFrame');
  });
  render(
    <loaded.ModalFrame
      visible
      onClose={() => undefined}
      closeAction="settings.modal.close"
      testID="modal"
    >
      {null}
    </loaded.ModalFrame>,
  );
  return StyleSheet.flatten(screen.getByTestId('modal-keyboard').props.style);
}

describe('ModalFrame', () => {
  it('pads the keyboard-avoiding view on iOS', () => {
    expect(keyboardAvoidingStyleOn('ios')).toHaveProperty('paddingBottom', 0);
  });

  it('leaves the keyboard-avoiding view unpadded on Android', () => {
    expect(keyboardAvoidingStyleOn('android')).not.toHaveProperty('paddingBottom');
  });
});
