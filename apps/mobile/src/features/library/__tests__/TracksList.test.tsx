import { fireEvent, render, screen } from '@testing-library/react-native';

import type { ListRefresh } from '../refresh';
import { TracksList } from '../ui/TracksList';

function idleRefresh(): ListRefresh {
  return { onRefresh: jest.fn(), refreshing: false };
}

function renderEmptyTracksList(refresh: ListRefresh) {
  render(
    <TracksList
      tracks={[]}
      emptyLabel="No tracks yet"
      refresh={refresh}
      onPlay={jest.fn()}
      onPress={jest.fn()}
      onMore={jest.fn()}
      onRetry={jest.fn()}
      isRetrying={() => false}
      isPlaying={() => false}
    />,
  );
}

describe('library list shells — empty state', () => {
  it('shows the tracks empty label when there are no tracks', () => {
    renderEmptyTracksList(idleRefresh());

    expect(screen.getByText('No tracks yet')).toBeTruthy();
  });
});

describe('library list shells — pull to refresh', () => {
  it.each([['tracks', renderEmptyTracksList]])(
    'asks the %s list to refresh when the user pulls it down',
    (_name, renderList) => {
      const refresh = idleRefresh();
      renderList(refresh);

      fireEvent(screen.UNSAFE_getByProps({ refreshing: false }), 'refresh');

      expect(refresh.onRefresh).toHaveBeenCalledTimes(1);
    },
  );
});
