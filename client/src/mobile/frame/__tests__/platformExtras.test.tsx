import { render } from '@testing-library/react-native';

import { PlaybackShortcuts, SystemNavigationBar, TabsTitle, navigationBarStyleFor } from '@/mobile';

describe('platformExtras on native', () => {
  it('renders nothing for all three components', () => {
    const { toJSON } = render(
      <>
        <SystemNavigationBar scheme="dark" />
        <PlaybackShortcuts />
        <TabsTitle pathname="/library" />
      </>,
    );

    expect(toJSON()).toBeNull();
  });
});

describe('navigationBarStyleFor', () => {
  it('uses light icons on a dark scheme', () => {
    expect(navigationBarStyleFor('dark')).toBe('light');
  });

  it('uses dark icons on a light scheme', () => {
    expect(navigationBarStyleFor('light')).toBe('dark');
  });
});
