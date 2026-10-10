import { StyleSheet } from 'react-native';
import { render, screen } from '@testing-library/react-native';
import type * as SettingsModalExports from '@features/settings/ui/ConfirmModal';

const sharedReact = jest.requireActual('react');
const sharedReactNative = jest.requireActual('react-native');

function keyboardAvoidingStyleOn(os: 'ios' | 'android'): Record<string, unknown> {
  let loaded!: typeof SettingsModalExports;
  jest.isolateModules(() => {
    jest.doMock('react', () => sharedReact);
    jest.doMock('react-native', () => sharedReactNative);
    sharedReactNative.Platform.OS = os;
    loaded = jest.requireActual<typeof SettingsModalExports>('@features/settings/ui/ConfirmModal');
  });
  render(
    <loaded.SettingsModal visible onClose={() => undefined} testID="modal">
      {null}
    </loaded.SettingsModal>,
  );
  return StyleSheet.flatten(screen.getByTestId('modal-keyboard').props.style);
}

describe('SettingsModal', () => {
  it('pads the keyboard-avoiding view on iOS', () => {
    expect(keyboardAvoidingStyleOn('ios')).toHaveProperty('paddingBottom', 0);
  });

  it('leaves the keyboard-avoiding view unpadded on Android', () => {
    expect(keyboardAvoidingStyleOn('android')).not.toHaveProperty('paddingBottom');
  });
});
