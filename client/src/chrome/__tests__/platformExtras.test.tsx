import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { asPlaylistId } from '@shared/apiClient/ids';
import type { PlaybackContextValue } from '@shared/playback/playbackContextValue';
import { PlaybackContext } from '@shared/playback/usePlayback';
import { pageTitleFor } from '@shared/ui/navigation/tabRoutes';
import * as base from '@/chrome/platformExtras';
import * as android from '@/chrome/platformExtras.android';
import * as web from '@/chrome/platformExtras.web';

import { createMemoryLibraryPort } from '@features/library/__tests__/memoryLibraryPort';
import { LibraryPortProvider } from '@features/library/ports/LibraryPortProvider';

const mockNavigationBar = jest.fn();

jest.mock('expo-navigation-bar', () => ({
  NavigationBar: (props: { style: string }) => {
    mockNavigationBar(props);
    return null;
  },
}));

jest.mock('expo-router/head', () => {
  const { View } = jest.requireActual('react-native');
  return {
    __esModule: true,
    default: ({ children }: { children: unknown }) => <View>{children}</View>,
  };
});

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn() }),
}));

const port = createMemoryLibraryPort({
  playlists: [
    {
      id: asPlaylistId('p1'),
      name: 'Road Trip',
      track_count: 0,
      preview_artwork_urls: [],
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z',
      total_duration_seconds: 0,
      tracks: [],
    },
  ],
});

let client: QueryClient;

interface ProvidersProps {
  children: ReactNode;
}

function Providers({ children }: ProvidersProps) {
  return (
    <QueryClientProvider client={client}>
      <LibraryPortProvider port={port}>{children}</LibraryPortProvider>
    </QueryClientProvider>
  );
}

function playingWith(pause: () => void): PlaybackContextValue {
  return {
    status: 'playing',
    track: null,
    positionMs: 0,
    durationMs: 0,
    errorMessage: null,
    errorKind: null,
    play: jest.fn(),
    startQueue: jest.fn(),
    skipToQueueIndex: jest.fn(),
    reorderUpcoming: jest.fn(),
    appendToQueue: jest.fn(),
    insertNext: jest.fn(),
    skipNext: jest.fn(),
    skipPrevious: jest.fn(),
    removeQueueIndex: jest.fn(),
    pause,
    resume: jest.fn(),
    seekTo: jest.fn(),
    setRate: jest.fn(),
    stop: jest.fn(),
    retry: jest.fn(),
  };
}

function installWindowListeners(): jest.Mock {
  const addEventListener = jest.fn();
  Object.assign(window, { addEventListener, removeEventListener: jest.fn() });
  return addEventListener;
}

function keydownHandlerOn(addEventListener: jest.Mock): (event: unknown) => void {
  const call = addEventListener.mock.calls.find(([type]) => type === 'keydown');
  return call?.[1] as (event: unknown) => void;
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  mockNavigationBar.mockClear();
});

afterEach(() => {
  client.clear();
  Reflect.deleteProperty(window, 'addEventListener');
  Reflect.deleteProperty(window, 'removeEventListener');
});

describe('platformExtras default', () => {
  it('renders nothing for all three components', () => {
    const addEventListener = installWindowListeners();
    const { toJSON } = render(
      <>
        <base.SystemNavigationBar scheme="dark" />
        <base.PlaybackShortcuts />
        <base.TabsTitle pathname="/library" />
      </>,
    );
    expect(toJSON()).toBeNull();
    expect(mockNavigationBar).not.toHaveBeenCalled();
    expect(addEventListener).not.toHaveBeenCalled();
  });
});

describe('platformExtras.android', () => {
  it('renders the NavigationBar with a light style on a dark scheme', () => {
    render(<android.SystemNavigationBar scheme="dark" />);
    expect(mockNavigationBar).toHaveBeenCalledWith({ style: 'light' });
  });

  it('renders the NavigationBar with a dark style on a light scheme', () => {
    render(<android.SystemNavigationBar scheme="light" />);
    expect(mockNavigationBar).toHaveBeenCalledWith({ style: 'dark' });
  });

  it('renders nothing for shortcuts and title', () => {
    const { toJSON } = render(
      <>
        <android.PlaybackShortcuts />
        <android.TabsTitle pathname="/library" />
      </>,
    );
    expect(toJSON()).toBeNull();
  });
});

describe('platformExtras.web', () => {
  it('drives the playback context from keyboard shortcuts on the window', () => {
    const addEventListener = installWindowListeners();
    const pause = jest.fn();
    const playback = playingWith(pause);
    render(
      <PlaybackContext.Provider value={playback}>
        <web.PlaybackShortcuts />
      </PlaybackContext.Provider>,
    );

    keydownHandlerOn(addEventListener)({
      key: ' ',
      shiftKey: false,
      ctrlKey: false,
      metaKey: false,
      altKey: false,
      target: null,
      preventDefault: jest.fn(),
    });

    expect(pause).toHaveBeenCalledTimes(1);
  });

  it('renders the page title for the pathname', () => {
    const { toJSON } = render(<web.TabsTitle pathname="/discover" />);
    expect(JSON.stringify(toJSON())).toContain(pageTitleFor('/discover'));
  });

  it('renders the playlist name in the title on a playlist page', async () => {
    const { toJSON } = render(<web.TabsTitle pathname="/library/playlist/p1" />, {
      wrapper: Providers,
    });
    await waitFor(() =>
      expect(JSON.stringify(toJSON())).toContain(pageTitleFor('/library/playlist/p1', 'Road Trip')),
    );
  });

  it('renders nothing for the navigation bar', () => {
    const { toJSON } = render(<web.SystemNavigationBar scheme="dark" />);
    expect(toJSON()).toBeNull();
  });
});
