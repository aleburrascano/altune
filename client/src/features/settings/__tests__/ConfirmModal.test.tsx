import { StyleSheet } from 'react-native';
import { render, screen } from '@testing-library/react-native';
import type * as ModalFrameExports from '@features/settings/ui/ConfirmModal';

const sharedReact = jest.requireActual('react');
const sharedReactNative = jest.requireActual('react-native');

function keyboardAvoidingStyleOn(os: 'ios' | 'android'): Record<string, unknown> {
  let loaded!: typeof ModalFrameExports;
  jest.isolateModules(() => {
    jest.doMock('react', () => sharedReact);
    jest.doMock('react-native', () => sharedReactNative);
    sharedReactNative.Platform.OS = os;
    loaded = jest.requireActual<typeof ModalFrameExports>('@features/settings/ui/ConfirmModal');
  });
  render(
    <loaded.ModalFrame visible onClose={() => undefined} testID="modal">
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
