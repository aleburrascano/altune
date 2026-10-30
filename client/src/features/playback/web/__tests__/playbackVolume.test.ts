import {
  bindAudioVolume,
  toVolume,
  usePlaybackVolumeStore,
} from '@features/playback/web/playbackVolume';

const QUIET = 0.25;
const MID = 0.4;
const HALF = 0.5;
const ABOVE_MAX = 1.5;
const BELOW_MIN = -0.2;

function fakeAudio(): HTMLAudioElement {
  return { volume: 1 } as HTMLAudioElement;
}

afterEach(() => {
  usePlaybackVolumeStore.setState({ volume: toVolume(1) });
});

describe('bindAudioVolume — the audio element follows the stored volume', () => {
  it('applies the stored volume to an audio element when it binds', () => {
    usePlaybackVolumeStore.setState({ volume: toVolume(MID) });
    const audio = fakeAudio();
    bindAudioVolume(audio);
    expect(audio.volume).toBe(MID);
  });

  it('follows later changes until the cleanup runs', () => {
    const audio = fakeAudio();
    const unbind = bindAudioVolume(audio);
    usePlaybackVolumeStore.getState().setVolume(toVolume(QUIET));
    expect(audio.volume).toBe(QUIET);
    unbind();
    usePlaybackVolumeStore.getState().setVolume(toVolume(HALF));
    expect(audio.volume).toBe(QUIET);
  });
});

describe('toVolume — the one parser that makes a volume the audio element accepts', () => {
  it('clamps values above 1 and below 0', () => {
    expect(toVolume(ABOVE_MAX)).toBe(1);
    expect(toVolume(BELOW_MIN)).toBe(0);
  });

  it('reads a value that is not a number as silence', () => {
    expect(toVolume(Number.NaN)).toBe(0);
  });
});
