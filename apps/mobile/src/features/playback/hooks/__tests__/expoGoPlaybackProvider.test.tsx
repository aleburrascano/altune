import React from 'react';
import { act, renderHook } from '@testing-library/react-native';

import { asTrackId } from '@shared/api-client/ids';
import { usePlayback } from '@shared/playback/usePlayback';
import type { PlaybackContextValue, PlaybackTrack } from '@shared/playback/types';

import { ExpoGoPlaybackProvider } from '../expoGoPlaybackProvider';

const devFlag = globalThis as unknown as { __DEV__: boolean };

function buildTrack(): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: asTrackId('track-1') },
    title: 'Test Track',
    artist: 'Test Artist',
    artworkUrl: null,
  };
}

function renderControls(): PlaybackContextValue {
  const wrapper = ({ children }: { children: React.ReactNode }) => (
    <ExpoGoPlaybackProvider>{children}</ExpoGoPlaybackProvider>
  );
  return renderHook(() => usePlayback(), { wrapper }).result.current;
}

type ControlCase = [string, (controls: PlaybackContextValue) => void | Promise<void>];

const CONTROLS_EXPO_GO_CANNOT_HONOUR: readonly ControlCase[] = [
  ['play', (c) => c.play(buildTrack())],
  ['startQueue', (c) => c.startQueue([buildTrack()], 0)],
  ['reorderUpcoming', (c) => c.reorderUpcoming([buildTrack()])],
  ['appendToQueue', (c) => c.appendToQueue(buildTrack())],
  ['insertNext', (c) => c.insertNext(buildTrack(), 0)],
  ['skipToQueueIndex', (c) => c.skipToQueueIndex(1)],
  ['removeQueueIndex', (c) => c.removeQueueIndex(1)],
  ['pause', (c) => c.pause()],
  ['resume', (c) => c.resume()],
  ['seekTo', (c) => c.seekTo(1_000)],
  ['setRate', (c) => c.setRate(1.5)],
  ['stop', (c) => c.stop()],
  ['retry', (c) => c.retry()],
];

describe('ExpoGoPlaybackProvider', () => {
  let warnSpy: jest.SpyInstance<void, Parameters<typeof console.warn>>;

  beforeEach(() => {
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    warnSpy.mockRestore();
  });

  it.each(CONTROLS_EXPO_GO_CANNOT_HONOUR)(
    'warns in dev that %s was skipped, naming it and the dev-build fix',
    async (control, invoke) => {
      const controls = renderControls();

      await act(async () => {
        await invoke(controls);
      });

      expect(warnSpy).toHaveBeenCalledTimes(1);
      expect(warnSpy).toHaveBeenCalledWith(
        expect.stringContaining(`${control}() skipped: audio is disabled in Expo Go`),
      );
      expect(warnSpy).toHaveBeenCalledWith(expect.stringContaining('expo-dev-client'));
    },
  );

  it('stays silent for skipNext, which really moves the queue', async () => {
    const controls = renderControls();

    await act(async () => {
      await controls.skipNext();
    });

    expect(warnSpy).not.toHaveBeenCalled();
  });

  it('stays silent for skipPrevious, which really moves the queue', async () => {
    const controls = renderControls();

    await act(async () => {
      await controls.skipPrevious();
    });

    expect(warnSpy).not.toHaveBeenCalled();
  });

  it('stays silent in a production build, where __DEV__ is false', async () => {
    const wasDev = devFlag.__DEV__;
    devFlag.__DEV__ = false;
    try {
      const controls = renderControls();

      await act(async () => {
        await controls.startQueue([buildTrack()], 0);
      });

      expect(warnSpy).not.toHaveBeenCalled();
    } finally {
      devFlag.__DEV__ = wasDev;
    }
  });
});
