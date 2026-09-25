import { act, renderHook } from '@testing-library/react-native';
import { AppState, type AppStateStatus } from 'react-native';
import TrackPlayer from 'react-native-track-player';

import { asTrackId } from '@shared/api-client/ids';
import { saveQueueState } from '@shared/api-client/playback';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';

import { useQueueResume } from '../hooks/useQueueResume';
import { loadNativeQueue } from '../loadNativeTrack';

import { libraryTrack } from './fixtures';

jest.mock('@shared/api-client/playback', () => ({
  getQueueState: jest.fn(async () => null),
  saveQueueState: jest.fn(),
}));
jest.mock('@shared/api-client/tracks', () => ({ getAllTracks: jest.fn() }));
jest.mock('@shared/api-client/audio', () => ({
  audioStreamUrl: (id: string) => `https://api.example/audio/${id}`,
  audioRequestHeaders: jest.fn(async () => ({})),
  fetchAudioUrls: jest.fn(async () => []),
}));

const player = TrackPlayer as unknown as Record<string, jest.Mock>;
const mockedSave = saveQueueState as jest.MockedFunction<typeof saveQueueState>;

describe('useQueueResume save — a save issued as the drain finishes is not dropped', () => {
  it.each(Array.from({ length: 30 }, (_, offset) => offset))(
    'saves the latest position when the second trigger lands %i ticks after the first',
    async (offset) => {
      let position = 1;
      const listeners: ((state: AppStateStatus) => void)[] = [];
      jest.spyOn(AppState, 'addEventListener').mockImplementation((_type, listener) => {
        listeners.push(listener as (state: AppStateStatus) => void);
        return { remove: jest.fn() };
      });
      player.getActiveTrack!.mockImplementation(async () => ({ id: 'library:a0' }));
      player.getProgress!.mockImplementation(async () => ({
        position,
        duration: 300,
        buffered: 0,
      }));
      mockedSave.mockReset().mockResolvedValue(undefined);
      const tracks = [libraryTrack({ source: { kind: 'library', trackId: asTrackId('a0') } })];
      useQueueStore.getState().clearQueue();
      useQueueStore.getState().loadQueue(tracks, 0, null);
      await loadNativeQueue(orderedQueueTracks(useQueueStore.getState()), 0, { autoplay: false });
      renderHook(() => useQueueResume());
      await act(async () => {
        for (let i = 0; i < 20; i++) await Promise.resolve();
      });

      await act(async () => {
        for (const l of [...listeners]) l('background');
        for (let tick = 0; tick < offset; tick++) await Promise.resolve();
        position = 99;
        for (const l of [...listeners]) l('background');
        for (let i = 0; i < 60; i++) await Promise.resolve();
      });

      const last = mockedSave.mock.calls.at(-1)![0];
      expect(last.position_ms).toBe(99_000);
    },
  );
});
