import { KeyboardAvoidingView } from 'react-native';
import { render, screen } from '@testing-library/react-native';
import type * as SettingsModalExports from '../ui/SettingsModal';

const sharedReact = jest.requireActual('react');
const sharedReactNative = jest.requireActual('react-native');

function keyboardAvoidingBehaviorOn(os: 'ios' | 'android'): unknown {
  let loaded!: typeof SettingsModalExports;
  jest.isolateModules(() => {
    jest.doMock('react', () => sharedReact);
    jest.doMock('react-native', () => sharedReactNative);
    sharedReactNative.Platform.OS = os;
    loaded = jest.requireActual<typeof SettingsModalExports>('../ui/SettingsModal');
  });
  render(
    <loaded.SettingsModal visible onClose={() => undefined}>
      {null}
    </loaded.SettingsModal>,
  );
  return screen.UNSAFE_getByType(KeyboardAvoidingView).props.behavior;
}

describe('SettingsModal', () => {
  it('pads the keyboard-avoiding view on iOS', () => {
    expect(keyboardAvoidingBehaviorOn('ios')).toBe('padding');
  });

  it('leaves the keyboard-avoiding view unpadded on Android', () => {
    expect(keyboardAvoidingBehaviorOn('android')).toBeUndefined();
  });
});
