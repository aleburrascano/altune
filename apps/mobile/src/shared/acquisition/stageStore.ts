/**
 * Ephemeral per-track acquisition stage, keyed by track id.
 *
 * The current pipeline stage ('search'/'download'/…) is transient UI state, not
 * a server field — the track row the API returns has no stage. Storing it here
 * (instead of on the cached TrackResponse) means a library refetch/poll can't
 * wipe it, so "Downloading…" persists instead of flickering back to "Pending".
 * Progress events set it; completed/failed clear it.
 */

import { create } from 'zustand';

interface AcquisitionStageState {
  stages: Record<string, string>;
  setStage: (trackId: string, stage: string) => void;
  clearStage: (trackId: string) => void;
}

export const useAcquisitionStageStore = create<AcquisitionStageState>((set) => ({
  stages: {},
  setStage: (trackId, stage) => set((s) => ({ stages: { ...s.stages, [trackId]: stage } })),
  clearStage: (trackId) =>
    set((s) => {
      if (!(trackId in s.stages)) return s;
      const next = { ...s.stages };
      delete next[trackId];
      return { stages: next };
    }),
}));

/** Reactive read for components — re-renders only when this track's stage changes. */
export function useTrackStage(trackId: string): string | undefined {
  return useAcquisitionStageStore((s) => s.stages[trackId]);
}

/** Imperative setters for the event dispatcher (called outside React). */
export function setTrackStage(trackId: string, stage: string): void {
  useAcquisitionStageStore.getState().setStage(trackId, stage);
}

export function clearTrackStage(trackId: string): void {
  useAcquisitionStageStore.getState().clearStage(trackId);
}
