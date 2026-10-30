import { create } from 'zustand';

type PlaybackVolumeState = {
  volume: number;
  setVolume: (volume: number) => void;
};

function clampVolume(volume: number): number {
  return Math.min(1, Math.max(0, volume));
}

export const usePlaybackVolumeStore = create<PlaybackVolumeState>((set) => ({
  volume: 1,
  setVolume: (volume) => set({ volume: clampVolume(volume) }),
}));

export function bindAudioVolume(audio: HTMLAudioElement): () => void {
  audio.volume = usePlaybackVolumeStore.getState().volume;
  return usePlaybackVolumeStore.subscribe((state) => {
    audio.volume = state.volume;
  });
}
