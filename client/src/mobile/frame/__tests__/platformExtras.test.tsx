import { render } from '@testing-library/react-native';

import { PlaybackShortcuts, SystemNavigationBar, TabsTitle } from '@/mobile';

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
