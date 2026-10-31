import TrackPlayer from 'react-native-track-player';

import { audioStreamUrl } from '@shared/apiClient/audio';
import { asTrackId } from '@shared/apiClient/ids';
import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/playbackTrack';

import { createNativePlaybackActions } from '@features/playback/native/createNativePlaybackActions';
import { ensurePlayerSetup } from '@features/playback/native/initPlayer';
import { forgetAllSwaps } from '@features/playback/native/nativeTrackSwap';
import { httpPlaybackPort } from '@features/playback/adapters/http/httpPlaybackPort';
import { installPlaybackPort } from '@features/playback/ports/activePlaybackPort';
import { signInTestUser } from '@testing/signInTestUser';

import { libraryTrack, previewTrack } from '@features/playback/__tests__/fixtures';
import { createMemoryPlaybackPort } from '@features/playback/__tests__/memoryPlaybackPort';

const { __http } = require('@testing/doubles/fetch.js');

const HEADERS_MS = 400;
const PRESIGN_MS = 900;
const AUDIO_URLS_DEADLINE_MS = 2500;
const SETTLE_MS = 30_000;

const nativePlay = TrackPlayer.play as jest.Mock;

let restorePort: (() => void) | null = null;

function after<T>(ms: number, value: T): Promise<T> {
  return new Promise((resolve) => setTimeout(() => resolve(value), ms));
}

function installSlowMemoryPort(): void {
  const port = createMemoryPlaybackPort({
    audioUrls: { 'trk-1': { url: 'https://cdn.example/trk-1.mp3', version: 'v1' } },
  });
  restorePort = installPlaybackPort({
    ...port,
    audioRequestHeaders: () => after(HEADERS_MS, {}),
    fetchAudioUrls: async (ids) => after(PRESIGN_MS, await port.fetchAudioUrls(ids)),
  });
}

async function msUntilNativePlay(start: () => Promise<void>): Promise<number> {
  let playedAt: number | null = null;
  nativePlay.mockImplementationOnce(async () => {
    playedAt = Date.now();
  });
  const startedAt = Date.now();
  const done = start();
  await jest.advanceTimersByTimeAsync(SETTLE_MS);
  await done;
  if (playedAt === null) throw new Error('TrackPlayer.play was never called');
  return playedAt - startedAt;
}

function playThroughControls(track: PlaybackTrack): () => Promise<void> {
  const { controls } = createNativePlaybackActions(() => undefined);
  return () => controls.play(track);
}

beforeAll(async () => {
  await ensurePlayerSetup();
});

beforeEach(async () => {
  await signInTestUser();
  useQueueStore.getState().clearQueue();
  forgetAllSwaps();
  jest.useFakeTimers();
});

afterEach(() => {
  jest.useRealTimers();
  restorePort?.();
  restorePort = null;
});

describe('time from tapping play to TrackPlayer.play, with simulated network latency', () => {
  it('a preview track starts with no network wait', async () => {
    installSlowMemoryPort();

    expect(await msUntilNativePlay(playThroughControls(previewTrack()))).toBe(0);
  });

  it('a library track waits only for the slower of headers and presign, because they run in parallel', async () => {
    installSlowMemoryPort();

    const elapsed = await msUntilNativePlay(playThroughControls(libraryTrack()));

    expect(elapsed).toBe(Math.max(HEADERS_MS, PRESIGN_MS));
  });

  it('a stalled presign on a weak signal holds play only until the audio-urls deadline, then streams', async () => {
    restorePort = installPlaybackPort(httpPlaybackPort);
    __http.hang('POST /v1/audio-urls');
    const track = libraryTrack({ source: { kind: 'library', trackId: asTrackId('trk-1') } });

    const elapsed = await msUntilNativePlay(playThroughControls(track));

    expect(elapsed).toBe(AUDIO_URLS_DEADLINE_MS);
    const [added] = (TrackPlayer.add as jest.Mock).mock.calls.at(-1)!;
    expect(added.url).toBe(audioStreamUrl(asTrackId('trk-1')));
  });
});
