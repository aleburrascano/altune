// #786: the playlist id arrives from a deep-link route param, so it is untrusted. A value that
// isn't a plausible id shape must never reach a request path; the screen treats it as no id.

import type { ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, waitFor } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';

import { PlaylistDetailScreen } from '../ui/PlaylistDetailScreen';

const { __http } = require('../../../../jest/doubles/fetch.js');

let mockParams: { id?: string } = {};
const mockReplace = jest.fn();
jest.mock('expo-router', () => ({
  useLocalSearchParams: () => mockParams,
  useRouter: () => ({
    replace: mockReplace,
    push: jest.fn(),
    back: jest.fn(),
    canGoBack: () => false,
  }),
}));

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

// Playback providers are irrelevant to routing; stub them so the screen can mount on its own.
jest.mock('@shared/playback/usePlayback', () => ({
  usePlayback: () => ({ status: 'idle', source: null }),
}));
jest.mock('@shared/playback/useQueuePlayback', () => ({
  useQueuePlayback: () => ({}),
}));

function playlistRequests(): string[] {
  return (__http.requests as { path: string }[])
    .map((r) => r.path)
    .filter((path) => path.startsWith('/v1/playlists'));
}

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity },
      mutations: { gcTime: Infinity },
    },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe('PlaylistDetailScreen route param', () => {
  beforeEach(() => {
    mockReplace.mockClear();
    (supabase.auth.getSession as jest.Mock).mockResolvedValue({
      data: { session: { access_token: 'tok' } },
      error: null,
    });
  });

  it.each(['p1/tracks', '../p1', 'p1?x=1', 'p1#frag', 'p1%2Ftracks'])(
    'redirects to the library and requests nothing for the malformed id %p',
    async (id) => {
      mockParams = { id };

      render(<PlaylistDetailScreen />, { wrapper });
      await Promise.resolve();

      expect(mockReplace).toHaveBeenCalledWith('/library');
      expect(playlistRequests()).toEqual([]);
    },
  );

  it('fetches the playlist for a well-formed id, so the redirect above is not a blanket one', async () => {
    mockParams = { id: 'p1' };
    __http.reply('GET /v1/playlists/p1', { status: 404, json: { message: 'not found' } });

    render(<PlaylistDetailScreen />, { wrapper });

    await waitFor(() => expect(playlistRequests()).toEqual(['/v1/playlists/p1']));
    expect(mockReplace).not.toHaveBeenCalled();
  });
});
