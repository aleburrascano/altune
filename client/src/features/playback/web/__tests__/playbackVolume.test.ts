import { bindAudioVolume, usePlaybackVolumeStore } from '@features/playback/web/playbackVolume';

function fakeAudio(): HTMLAudioElement {
  return { volume: 1 } as HTMLAudioElement;
}

afterEach(() => {
  usePlaybackVolumeStore.setState({ volume: 1 });
});

describe('bindAudioVolume — the audio element follows the stored volume', () => {
  it('applies the stored volume to an audio element when it binds', () => {
    usePlaybackVolumeStore.setState({ volume: 0.4 });
    const audio = fakeAudio();
    bindAudioVolume(audio);
    expect(audio.volume).toBe(0.4);
  });

  it('follows later changes until the cleanup runs', () => {
    const audio = fakeAudio();
    const unbind = bindAudioVolume(audio);
    usePlaybackVolumeStore.getState().setVolume(0.25);
    expect(audio.volume).toBe(0.25);
    unbind();
    usePlaybackVolumeStore.getState().setVolume(0.5);
    expect(audio.volume).toBe(0.25);
  });
});

describe('setVolume — keeps the value the audio element accepts', () => {
  it('clamps values above 1 and below 0', () => {
    const { setVolume } = usePlaybackVolumeStore.getState();
    setVolume(1.5);
    expect(usePlaybackVolumeStore.getState().volume).toBe(1);
    setVolume(-0.2);
    expect(usePlaybackVolumeStore.getState().volume).toBe(0);
  });
});
