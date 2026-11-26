import { fireEvent, render, screen } from '@testing-library/react-native';
import type { ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { asTrackId } from '@shared/catalog';
import { PlaybackContext } from '@shared/playback';
import type { PlaybackContextValue } from '@shared/playback';
import { makeIdlePlayback } from '@testing/factories';
import { DiscoverPortProvider, DiscoverScreen } from '@features/discovery';
import type { DiscoverPort } from '@features/discovery';
import { discoverEnv } from '@features/discovery/__tests__/discoverEnv';

const port: DiscoverPort = {
  searchDiscovery: jest.fn(),
  suggestDiscovery: jest.fn().mockResolvedValue({ suggestions: [] }),
  listSearchHistory: jest.fn().mockResolvedValue({ items: [] }),
  recordSearchHistory: jest.fn().mockResolvedValue(undefined),
  searchUnreleasedTracks: jest.fn().mockResolvedValue({ items: [] }),
};

function playbackFixture(): PlaybackContextValue {
  return makeIdlePlayback({
    status: 'paused',
    track: {
      source: { kind: 'library', trackId: asTrackId('track-1') },
      title: 'Test Track',
      artist: 'Test Artist',
      artworkUrl: null,
    },
    durationMs: 200_000,
  });
}

function renderDiscover(env: ReturnType<typeof discoverEnv>['env']): ReactElement {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={queryClient}>
      <PlaybackContext.Provider value={playbackFixture()}>
        <DiscoverPortProvider port={port}>
          <DiscoverScreen {...env} />
        </DiscoverPortProvider>
      </PlaybackContext.Provider>
    </QueryClientProvider>
  );
}

describe('DiscoverScreen tap-to-dismiss-keyboard area', () => {
  it('dismisses the keyboard when the screen body is tapped', () => {
    const fake = discoverEnv();
    render(renderDiscover(fake.env));

    fireEvent.press(screen.getByTestId('discover-screen-body'));

    expect(fake.dismissKeyboard).toHaveBeenCalledTimes(1);
  });
});
