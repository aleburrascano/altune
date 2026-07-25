import { create } from 'zustand';

interface PlaybackErrorState {
  key: string | null;
  message: string | null;
  report: (key: string, message: string) => void;
  clear: () => void;
}

export const usePlaybackErrorStore = create<PlaybackErrorState>((set) => ({
  key: null,
  message: null,
  report: (key, message) => set({ key, message }),
  clear: () => set({ key: null, message: null }),
}));

export function reportPlaybackError(key: string, message: string): void {
  usePlaybackErrorStore.getState().report(key, message);
}

export function clearPlaybackError(): void {
  usePlaybackErrorStore.getState().clear();
}

export function usePlaybackErrorFor(key: string | null): string | null {
  return usePlaybackErrorStore((s) => (key != null && s.key === key ? s.message : null));
}
