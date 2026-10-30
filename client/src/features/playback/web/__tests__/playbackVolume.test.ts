import { bindAudioVolume, usePlaybackVolumeStore } from '@features/playback/web/playbackVolume';

const QUIET = 0.25;
const MID = 0.4;
const HALF = 0.5;
const ABOVE_MAX = 1.5;
const BELOW_MIN = -0.2;

function fakeAudio(): HTMLAudioElement {
  return { volume: 1 } as HTMLAudioElement;
}

afterEach(() => {
  usePlaybackVolumeStore.setState({ volume: 1 });
});

describe('bindAudioVolume — the audio element follows the stored volume', () => {
  it('applies the stored volume to an audio element when it binds', () => {
    usePlaybackVolumeStore.setState({ volume: MID });
    const audio = fakeAudio();
    bindAudioVolume(audio);
    expect(audio.volume).toBe(MID);
  });

  it('follows later changes until the cleanup runs', () => {
    const audio = fakeAudio();
    const unbind = bindAudioVolume(audio);
    usePlaybackVolumeStore.getState().setVolume(QUIET);
    expect(audio.volume).toBe(QUIET);
    unbind();
    usePlaybackVolumeStore.getState().setVolume(HALF);
    expect(audio.volume).toBe(QUIET);
  });
});

describe('setVolume — keeps the value the audio element accepts', () => {
  it('clamps values above 1 and below 0', () => {
    const { setVolume } = usePlaybackVolumeStore.getState();
    setVolume(ABOVE_MAX);
    expect(usePlaybackVolumeStore.getState().volume).toBe(1);
    setVolume(BELOW_MIN);
    expect(usePlaybackVolumeStore.getState().volume).toBe(0);
  });
});
