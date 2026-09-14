import { Event } from 'react-native-track-player';

import { useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';

import { usePlaybackErrorStore } from '../playbackErrorStore';
import { playbackService } from '../service';

import { previewTrack } from './fixtures';

const { __player } = jest.requireMock('react-native-track-player');

type PlaybackErrorHandler = (data: { code: string; message: string }) => void;

async function playbackErrorHandler(): Promise<PlaybackErrorHandler> {
  await playbackService();
  const registration = __player
    .calls('addEventListener')
    .find(([event]: [unknown]) => event === Event.PlaybackError);
  if (!registration) throw new Error('no PlaybackError listener was registered');
  return registration[1];
}

afterEach(() => {
  usePlaybackErrorStore.getState().clear();
  useQueueStore.getState().clearQueue();
});

describe('playbackService — a native PlaybackError never stores a signed URL or token', () => {
  it('reports the failing track with the tokenized stream URL redacted', async () => {
    const track = previewTrack();
    useQueueStore.getState().loadQueue([track], 0, null);
    const handler = await playbackErrorHandler();
    const streamUrl =
      'https://audio.altune.example/stream/trk-1?X-Amz-Signature=deadbeef&token=s3cr3t-token';

    handler({ code: 'android-io-bad-http-status', message: `Response code: 403 url=${streamUrl}` });

    await new Promise((resolve) => setImmediate(resolve));
    const stored = usePlaybackErrorStore.getState();
    expect(stored.key).toBe(trackKey(track));
    expect(stored.message).not.toMatch(/s3cr3t-token|deadbeef|audio\.altune\.example/);
    expect(stored.message).toBe('Response code: 403 url=[redacted url]');
  });
});
