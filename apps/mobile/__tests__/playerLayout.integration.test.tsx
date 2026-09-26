import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Text } from 'react-native';
import type { ReactNode } from 'react';
import { act, renderRouter, screen } from 'expo-router/testing-library';

import { PlaybackContext } from '@shared/playback/PlaybackContext';
import type { PlaybackContextValue } from '@shared/playback/types';

import PlayerLayout from '../src/app/player/_layout';

const { __http } = require('../jest/doubles/fetch.js');

jest.mock('decode-uri-component', () => ({
  __esModule: true,
  default: (s: string) => {
    try {
      return decodeURIComponent(s);
    } catch {
      return s;
    }
  },
}));

let mockWindowWidth = 1440;
jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockWindowWidth, height: 900, scale: 2, fontScale: 1 }),
}));

const RN = require('react-native');
const NATIVE_OS: string = RN.Platform.OS;

const IDLE = {
  status: 'idle',
  track: null,
  positionMs: 0,
  durationMs: 0,
  errorMessage: null,
  errorKind: null,
} as unknown as PlaybackContextValue;

let client: QueryClient;

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  __http.reply('GET /v1/playlists', { status: 200, json: { items: [], total: 0 } });
});

afterEach(() => {
  RN.Platform.OS = NATIVE_OS;
  mockWindowWidth = 1440;
  client.clear();
});

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={client}>
      <PlaybackContext.Provider value={IDLE}>{children}</PlaybackContext.Provider>
    </QueryClientProvider>
  );
}

const ROUTES = {
  'player/_layout': PlayerLayout,
  'player/index': () => <Text>player-screen</Text>,
  'player/queue': () => <Text>queue-screen</Text>,
  'player/lyrics': () => <Text>lyrics-screen</Text>,
};

async function openPlayer(initialUrl: string, { os, width }: { os: string; width: number }) {
  RN.Platform.OS = os;
  mockWindowWidth = width;
  const result = renderRouter(ROUTES, { initialUrl, wrapper });
  await act(async () => {});
  return result;
}

describe('player layout: a page beside the sidebar in wide web layout, a modal otherwise', () => {
  it('shows the sidebar and player bar alongside the queue page on a 1440px web window', async () => {
    await openPlayer('/player/queue', { os: 'web', width: 1440 });

    expect(screen.getByText('queue-screen')).toBeTruthy();
    expect(screen.getByTestId('sidebar')).toBeTruthy();
    expect(screen.getByTestId('player-bar')).toBeTruthy();
  });

  it('shows the sidebar and player bar alongside the lyrics page on a 1440px web window', async () => {
    await openPlayer('/player/lyrics', { os: 'web', width: 1440 });

    expect(screen.getByText('lyrics-screen')).toBeTruthy();
    expect(screen.getByTestId('sidebar')).toBeTruthy();
    expect(screen.getByTestId('player-bar')).toBeTruthy();
  });

  it('shows neither sidebar nor player bar around the queue page on a 999px web window', async () => {
    await openPlayer('/player/queue', { os: 'web', width: 999 });

    expect(screen.getByText('queue-screen')).toBeTruthy();
    expect(screen.queryByTestId('sidebar')).toBeNull();
    expect(screen.queryByTestId('player-bar')).toBeNull();
  });

  it('shows neither sidebar nor player bar around the queue page on native', async () => {
    await openPlayer('/player/queue', { os: NATIVE_OS, width: 1440 });

    expect(screen.getByText('queue-screen')).toBeTruthy();
    expect(screen.queryByTestId('sidebar')).toBeNull();
    expect(screen.queryByTestId('player-bar')).toBeNull();
  });
});
