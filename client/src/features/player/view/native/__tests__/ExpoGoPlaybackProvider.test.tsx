import React from 'react';
import { act, renderHook } from '@testing-library/react-native';
import { asTrackId } from '@shared/catalog';
import { usePlayback } from '@shared/playback';
import type { PlaybackContextValue, PlaybackTrack } from '@shared/playback';
import { ExpoGoPlaybackProvider } from '@features/player';
import { recordLogs, type RecordedLogs } from '@testing/fakes/log/inMemoryLogger';

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
  let logs: RecordedLogs;

  beforeEach(() => {
    logs = recordLogs();
  });

  it.each(CONTROLS_EXPO_GO_CANNOT_HONOUR)(
    'warns in dev that %s was skipped, naming it and the dev-build fix',
    async (control, invoke) => {
      const controls = renderControls();

      await act(async () => {
        await invoke(controls);
      });

      expect(logs.warnings).toHaveLength(1);
      expect(logs.warnings).toContainEqual([
        expect.stringContaining(`${control}() skipped: audio is disabled in Expo Go`),
      ]);
      expect(logs.warnings).toContainEqual([expect.stringContaining('expo-dev-client')]);
    },
  );

  it('stays silent for skipNext, which really moves the queue', async () => {
    const controls = renderControls();

    await act(async () => {
      await controls.skipNext();
    });

    expect(logs.warnings).toEqual([]);
  });

  it('stays silent for skipPrevious, which really moves the queue', async () => {
    const controls = renderControls();

    await act(async () => {
      await controls.skipPrevious();
    });

    expect(logs.warnings).toEqual([]);
  });

  it('stays silent in a production build, where __DEV__ is false', async () => {
    const wasDev = devFlag.__DEV__;
    devFlag.__DEV__ = false;
    try {
      const controls = renderControls();

      await act(async () => {
        await controls.startQueue([buildTrack()], 0);
      });

      expect(logs.warnings).toEqual([]);
    } finally {
      devFlag.__DEV__ = wasDev;
    }
  });
});
