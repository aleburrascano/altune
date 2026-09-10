import { create } from 'zustand';

import { asTrackId, type TrackId } from '@shared/api-client/ids';
import type { AcquisitionStatus } from '@shared/api-client/types';

export type TrackStatus = {
  acquisitionStatus: AcquisitionStatus;
  failureMessage: string | null;
};

type TrackStatusState = {
  statuses: Record<string, TrackStatus>;
  identities: Record<string, string>;
  patch: (trackId: string, status: TrackStatus) => void;
  remove: (trackId: string) => void;
  link: (identity: string, trackId: string) => void;
  unlink: (identity: string) => void;
  reset: () => void;
};

export const useTrackStatusStore = create<TrackStatusState>((set) => ({
  statuses: {},
  identities: {},
  patch: (trackId, status) => set((s) => ({ statuses: { ...s.statuses, [trackId]: status } })),
  remove: (trackId) =>
    set((s) => {
      if (!(trackId in s.statuses)) return s;
      const next = { ...s.statuses };
      delete next[trackId];
      return { statuses: next };
    }),
  link: (identity, trackId) =>
    set((s) => ({ identities: { ...s.identities, [identity]: trackId } })),
  unlink: (identity) =>
    set((s) => {
      if (!(identity in s.identities)) return s;
      const next = { ...s.identities };
      delete next[identity];
      return { identities: next };
    }),
  reset: () => set({ statuses: {}, identities: {} }),
}));

export function trackIdentityKey(title: string, artist: string): string | null {
  const t = title.trim().toLowerCase();
  const a = artist.trim().toLowerCase();
  if (t.length === 0 || a.length === 0) return null;
  return `${t} ${a}`;
}

export function linkTrackIdentity(identity: string | null, trackId: string): void {
  if (identity === null) return;
  useTrackStatusStore.getState().link(identity, trackId);
}

export function unlinkTrackIdentity(identity: string | null): void {
  if (identity === null) return;
  useTrackStatusStore.getState().unlink(identity);
}

export function useTrackIdForIdentity(identity: string | null): TrackId | undefined {
  return useTrackStatusStore((s) => {
    if (identity === null) return undefined;
    const id = s.identities[identity];
    return id === undefined ? undefined : asTrackId(id);
  });
}

export function patchTrackStatus(trackId: string, status: TrackStatus): void {
  useTrackStatusStore.getState().patch(trackId, status);
}

export function removeTrackStatus(trackId: string): void {
  useTrackStatusStore.getState().remove(trackId);
}

export function useTrackStatus(trackId: string | null): TrackStatus | undefined {
  return useTrackStatusStore((s) => (trackId === null ? undefined : s.statuses[trackId]));
}
