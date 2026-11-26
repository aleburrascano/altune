import { act, render } from '@testing-library/react-native';

import { PlaybackContext } from '@shared/playback';
import { makeIdlePlayback } from '@testing/factories';
import { PlaybackShortcuts, SystemNavigationBar } from '@/mobile';

describe('platformExtras on web', () => {
  it('drives the playback context from keyboard shortcuts on the window', () => {
    const pause = jest.fn();
    render(
      <PlaybackContext.Provider value={makeIdlePlayback({ status: 'playing', pause })}>
        <PlaybackShortcuts />
      </PlaybackContext.Provider>,
    );

    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: ' ' }));
    });

    expect(pause).toHaveBeenCalledTimes(1);
  });

  it('renders nothing for the navigation bar', () => {
    const { toJSON } = render(<SystemNavigationBar scheme="dark" />);

    expect(toJSON()).toBeNull();
  });
});
