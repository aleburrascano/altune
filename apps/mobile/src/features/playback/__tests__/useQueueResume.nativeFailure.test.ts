import { act, renderHook } from '@testing-library/react-native';
import TrackPlayer from 'react-native-track-player';

import { getQueueState } from '@shared/api-client/playback';
import { getAllTracks } from '@shared/api-client/tracks';
import type { TrackResponse } from '@shared/api-client/types';
import { useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';

import { useQueueResume } from '../hooks/useQueueResume';
import { usePlaybackErrorStore } from '../playbackErrorStore';

jest.mock('@shared/api-client/playback', () => ({
  getQueueState: jest.fn(),
  saveQueueState: jest.fn(async () => undefined),
}));
jest.mock('@shared/api-client/tracks', () => ({ getTracks: jest.fn(), getAllTracks: jest.fn() }));
jest.mock('@shared/api-client/audio', () => ({
  audioStreamUrl: (id: string) => `https://api.example/audio/${id}`,
  audioRequestHeaders: jest.fn(async () => ({})),
  fetchAudioUrls: jest.fn(async () => []),
}));

const { __player } = jest.requireMock('react-native-track-player');

function trackResponse(id: string): TrackResponse {
  return {
    id,
    title: `Title ${id}`,
    artist: 'Artist',
    album: null,
    duration_seconds: 200,
    added_at: '2026-01-01T00:00:00Z',
    acquisition_status: 'ready',
    artwork_url: null,
    failure_reason: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
  } as TrackResponse;
}

const wire = {
  track_ids: ['x', 'y'],
  current_index: 1,
  position_ms: 5000,
  shuffled: false,
  repeat_mode: 'off',
  source: { kind: 'playlist', playlist_id: 'p1', name: 'Chill' },
  natural_order: ['x', 'y'],
};

async function restore(): Promise<void> {
  (getQueueState as jest.Mock).mockResolvedValue(wire);
  renderHook(() => useQueueResume());
  await act(async () => {
    for (let i = 0; i < 40; i++) await Promise.resolve();
  });
}

beforeEach(() => {
  useQueueStore.getState().clearQueue();
  usePlaybackErrorStore.getState().clear();
  (getAllTracks as jest.Mock).mockReset().mockResolvedValue(['x', 'y'].map(trackResponse));
  jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  jest.restoreAllMocks();
});

describe('useQueueResume restore, native stage failure (#2702)', () => {
  it('reports the failure against the current track when the native load rejects', async () => {
    __player.failNext('add', new Error('native add rejected'));

    await restore();

    const current = useQueueStore.getState().currentTrack();
    expect(current).not.toBeNull();
    const error = usePlaybackErrorStore.getState();
    expect(error.key).toBe(trackKey(current!));
    expect(error.message).toBe('native add rejected');
  });

  it('reports nothing when the native load succeeds', async () => {
    await restore();

    expect(usePlaybackErrorStore.getState().key).toBeNull();
  });

  it('reports nothing when the queue is replaced during the native load that then rejects', async () => {
    (TrackPlayer.add as jest.Mock).mockImplementationOnce(async () => {
      useQueueStore.getState().loadQueue([{ ...trackResponse('z'), id: 'z' } as never], 0, null);
      throw new Error('native add rejected late');
    });

    await restore();

    expect(useQueueStore.getState().currentTrack()).not.toBeNull();
    expect(usePlaybackErrorStore.getState().key).toBeNull();
  });
});
