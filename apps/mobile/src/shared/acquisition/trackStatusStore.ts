import { create } from 'zustand';

import type { AcquisitionStatus } from '@shared/api-client/types';

export type TrackStatus = {
  acquisitionStatus: AcquisitionStatus;
  failureMessage: string | null;
};

type TrackStatusState = {
  statuses: Record<string, TrackStatus>;
  patch: (trackId: string, status: TrackStatus) => void;
  remove: (trackId: string) => void;
  reset: () => void;
};

export const useTrackStatusStore = create<TrackStatusState>((set) => ({
  statuses: {},
  patch: (trackId, status) => set((s) => ({ statuses: { ...s.statuses, [trackId]: status } })),
  remove: (trackId) =>
    set((s) => {
      if (!(trackId in s.statuses)) return s;
      const next = { ...s.statuses };
      delete next[trackId];
      return { statuses: next };
    }),
  reset: () => set({ statuses: {} }),
}));

export function patchTrackStatus(trackId: string, status: TrackStatus): void {
  useTrackStatusStore.getState().patch(trackId, status);
}

export function removeTrackStatus(trackId: string): void {
  useTrackStatusStore.getState().remove(trackId);
}

export function useTrackStatus(trackId: string | null): TrackStatus | undefined {
  return useTrackStatusStore((s) => (trackId === null ? undefined : s.statuses[trackId]));
}
