import { render, screen } from '@testing-library/react-native';

import { adjustsKeyboardInsets, AuthHeroLayout } from '@shared/ui';
describe('AuthHeroLayout', () => {
  it('hands the platform keyboard-inset setting to its scroll view', () => {
    render(<AuthHeroLayout background={false}>{null}</AuthHeroLayout>);
    expect(screen.getByTestId('auth-hero-scroll').props.automaticallyAdjustKeyboardInsets).toBe(
      adjustsKeyboardInsets,
    );
  });
});
