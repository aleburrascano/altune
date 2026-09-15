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

describe('playbackService — a native PlaybackError is stored with a typed failure kind', () => {
  async function kindFor(code: string, message: string): Promise<unknown> {
    const track = previewTrack();
    useQueueStore.getState().loadQueue([track], 0, null);
    const handler = await playbackErrorHandler();

    handler({ code, message });

    await new Promise((resolve) => setImmediate(resolve));
    expect(usePlaybackErrorStore.getState().key).toBe(trackKey(track));
    return usePlaybackErrorStore.getState().kind;
  }

  it('tells a lost connection apart from an undecodable file', async () => {
    const network = await kindFor('android-io-network-connection-failed', 'Source error');
    const decode = await kindFor('android-decoding-failed', 'Source error');

    expect(network).toBe('network');
    expect(decode).toBe('decode');
  });

  it('classifies a refused (403) stream request as auth and a 404 as not found', async () => {
    expect(await kindFor('android-io-bad-http-status', 'Response code: 403')).toBe('auth');
    expect(await kindFor('android-io-bad-http-status', 'Response code: 404')).toBe('not_found');
  });

  it('stores unknown when the native event carries no code or message', async () => {
    const kind = await kindFor(undefined as unknown as string, undefined as unknown as string);

    expect(kind).toBe('unknown');
    expect(usePlaybackErrorStore.getState().message).toBe('Playback failed');
  });
});
