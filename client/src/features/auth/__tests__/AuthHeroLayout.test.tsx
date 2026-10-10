import { render, screen } from '@testing-library/react-native';
import type * as AuthHeroLayoutExports from '@features/auth/ui/hero/AuthHeroLayout';

const sharedReact = jest.requireActual('react');
const sharedReactNative = jest.requireActual('react-native');

function adjustsInsetsOn(os: 'ios' | 'android'): unknown {
  let loaded!: typeof AuthHeroLayoutExports;
  jest.isolateModules(() => {
    jest.doMock('react', () => sharedReact);
    jest.doMock('react-native', () => sharedReactNative);
    sharedReactNative.Platform.OS = os;
    loaded = jest.requireActual<typeof AuthHeroLayoutExports>(
      '@features/auth/ui/hero/AuthHeroLayout',
    );
  });
  render(<loaded.AuthHeroLayout background={false}>{null}</loaded.AuthHeroLayout>);
  return screen.getByTestId('auth-hero-scroll').props.automaticallyAdjustKeyboardInsets;
}

describe('AuthHeroLayout', () => {
  it('adjusts keyboard insets on iOS', () => {
    expect(adjustsInsetsOn('ios')).toBe(true);
  });

  it('does not adjust keyboard insets on Android', () => {
    expect(adjustsInsetsOn('android')).toBe(false);
  });
});
