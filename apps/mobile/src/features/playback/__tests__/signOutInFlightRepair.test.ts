import { asTrackId } from '@shared/api-client/ids';

import { repairActiveToStreaming } from '../nativeTrackSwap';
import { resetPlaybackForSignOut } from '../service';

import { libraryTrack } from './fixtures';

const { __player } = jest.requireMock('react-native-track-player');

const mockFetchAudioUrls = jest.fn();

jest.mock('@shared/api-client/audio', () => ({
  fetchAudioUrls: (...args: unknown[]) => mockFetchAudioUrls(...args),
  audioRequestHeaders: jest.fn().mockResolvedValue({ Authorization: 'Bearer tok-a' }),
}));

const A_TRACK = libraryTrack({ source: { kind: 'library', trackId: asTrackId('trk-of-a') } });

function heldPresign() {
  let release: (v: { url: string }[]) => void = () => undefined;
  mockFetchAudioUrls.mockReturnValueOnce(new Promise((resolve) => (release = resolve)));
  return (url: string) => release([{ url }]);
}

describe('streaming repair in flight at sign-out (#2704)', () => {
  beforeEach(() => mockFetchAudioUrls.mockReset());

  it('does not load or play the previous user track after the reset', async () => {
    const resolvePresign = heldPresign();
    const repair = repairActiveToStreaming(A_TRACK);
    await resetPlaybackForSignOut();
    resolvePresign('https://cdn.example/a.mp3');
    await repair;

    expect(__player.calls('load')).toHaveLength(0);
    expect(__player.calls('play')).toHaveLength(0);
  });

  it('a repair started after sign-out still loads and plays', async () => {
    await resetPlaybackForSignOut();
    heldPresign()('https://cdn.example/a.mp3');
    await repairActiveToStreaming(A_TRACK);

    expect(__player.calls('load')).toHaveLength(1);
    expect(__player.calls('play')).toHaveLength(1);
  });
});
