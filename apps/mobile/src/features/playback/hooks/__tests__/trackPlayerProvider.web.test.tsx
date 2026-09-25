import React from 'react';
import { act, renderHook } from '@testing-library/react-native';

import { asTrackId } from '@shared/api-client/ids';
import { usePlayback } from '@shared/playback/usePlayback';
import type { PlaybackTrack } from '@shared/playback/types';

import { TrackPlayerPlaybackProvider } from '../trackPlayerProvider.web';

function buildTrack(): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: asTrackId('track-1') },
    title: 'Test Track',
    artist: 'Test Artist',
    artworkUrl: null,
  };
}

function renderWeb() {
  const wrapper = ({ children }: { children: React.ReactNode }) => (
    <TrackPlayerPlaybackProvider>{children}</TrackPlayerPlaybackProvider>
  );
  return renderHook(() => usePlayback(), { wrapper }).result.current;
}

describe('TrackPlayerPlaybackProvider (web)', () => {
  it('starts idle with no track, like Expo Go', () => {
    const controls = renderWeb();

    expect(controls.status).toBe('idle');
    expect(controls.track).toBeNull();
  });

  it('warns instead of loading audio, never touching react-native-track-player', async () => {
    const warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    const controls = renderWeb();

    await act(async () => {
      await controls.play(buildTrack());
    });

    expect(warnSpy).toHaveBeenCalledWith(expect.stringContaining('play() skipped'));
    warnSpy.mockRestore();
  });
});
