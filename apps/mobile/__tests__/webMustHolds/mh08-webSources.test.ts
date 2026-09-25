describe('mh08: web playback sources come only from POST /v1/audio-urls', () => {
  it.skip('plays a library track without ever requesting GET /v1/tracks/{id}/audio (un-skipped by "web audio provider plays a library track")', () => {});
});

import React from 'react';
import { act, renderHook } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';
import { asTrackId } from '@shared/api-client/ids';
import { usePlayback } from '@shared/playback/usePlayback';
import { WebPlaybackProvider } from '@features/playback/hooks/webPlaybackProvider';

const { __http } = require('../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const PRESIGNED_URL = 'https://bucket.example/track-1.mp3?X-Amz-Signature=abc';

function fakeAudioElement(): HTMLAudioElement {
  const audio = {
    src: '',
    paused: true,
    ended: false,
    readyState: 0,
    currentTime: 0,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    removeAttribute: () => {
      audio.src = '';
    },
    load: () => undefined,
    pause: () => undefined,
    play: () => Promise.resolve(),
  };
  return audio as unknown as HTMLAudioElement;
}

describe('mh08: the web provider presigns every library source', () => {
  it('plays a library track without ever requesting GET /v1/tracks/{id}/audio', async () => {
    (supabase.auth.getSession as jest.Mock).mockResolvedValue({
      data: { session: { access_token: 'tok' } },
      error: null,
    });
    __http.reply('POST /v1/audio-urls', {
      status: 200,
      json: { urls: [{ track_id: 'track-1', url: PRESIGNED_URL, version: '1' }] },
    });
    const audio = fakeAudioElement();
    const wrapper = ({ children }: { children: React.ReactNode }) =>
      React.createElement(WebPlaybackProvider, { createAudio: () => audio }, children);
    const { result } = renderHook(() => usePlayback(), { wrapper });

    await act(() =>
      result.current.play({
        source: { kind: 'library', trackId: asTrackId('track-1') },
        title: 'A Title',
        artist: 'An Artist',
        artworkUrl: null,
      }),
    );

    const requested = __http.requests.map(
      (request: { method: string; path: string }) => `${request.method} ${request.path}`,
    );
    expect(requested).toEqual(['POST /v1/audio-urls']);
    expect(audio.src).toBe(PRESIGNED_URL);
  });
});
