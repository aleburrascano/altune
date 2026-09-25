import TrackPlayer from 'react-native-track-player';

import { asTrackId } from '@shared/api-client/ids';
import { useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { insertNativeTrackNext } from '../loadNativeTrack';

import { libraryTrack } from './fixtures';

jest.mock('@shared/api-client/audio', () => ({
  audioStreamUrl: (id: string) => `https://api.example/audio/${id}`,
  audioRequestHeaders: jest.fn(async () => ({})),
  fetchAudioUrls: jest.fn(async () => []),
}));

const player = TrackPlayer as unknown as Record<string, jest.Mock>;

function track(id: string): PlaybackTrack {
  return libraryTrack({ source: { kind: 'library', trackId: asTrackId(id) } });
}

const A = track('a');
const B = track('b');
const C = track('c');

afterEach(() => {
  player.add!.mockReset();
  player.getQueue!.mockReset();
  useQueueStore.getState().clearQueue();
});

describe('insertNativeTrackNext with a duplicate of the next track (#2703)', () => {
  it('adds the duplicate when native does not yet hold it', async () => {
    useQueueStore.getState().loadQueue([A, B, C], 0, null);
    let native: { id: string }[] = [A, B, C].map((t) => ({ id: trackKey(t) }));
    player.getQueue!.mockImplementation(async () => native);
    player.add!.mockImplementation(async (added: { id: string }, at: number) => {
      native = [...native.slice(0, at), added, ...native.slice(at)];
    });

    useQueueStore.getState().playNext(B);
    await insertNativeTrackNext(B, 1);

    expect(native.map((n) => n.id)).toEqual([A, B, B, C].map(trackKey));
  });
});
