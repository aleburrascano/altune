import { create } from 'zustand';

export type Volume = number & { readonly brand: 'Volume' };

export function toVolume(value: number): Volume {
  const clamped = Number.isNaN(value) ? 0 : Math.min(1, Math.max(0, value));
  return clamped as Volume;
}

type PlaybackVolumeState = {
  volume: Volume;
  setVolume: (volume: Volume) => void;
};

export const usePlaybackVolumeStore = create<PlaybackVolumeState>((set) => ({
  volume: toVolume(1),
  setVolume: (volume) => set({ volume }),
}));

export function bindAudioVolume(audio: HTMLAudioElement): () => void {
  audio.volume = usePlaybackVolumeStore.getState().volume;
  return usePlaybackVolumeStore.subscribe((state) => {
    audio.volume = state.volume;
  });
}
