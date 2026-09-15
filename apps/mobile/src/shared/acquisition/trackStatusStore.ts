import { create } from 'zustand';

import type { TrackId } from '@shared/api-client/ids';
import type { AcquisitionStatus } from '@shared/api-client/types';

export type TrackStatus = {
  acquisitionStatus: AcquisitionStatus;
  failureMessage: string | null;
};

type TrackStatusState = {
  statuses: Record<string, TrackStatus>;
  identities: Record<string, TrackId>;
  patch: (trackId: TrackId, status: TrackStatus) => void;
  remove: (trackId: TrackId) => void;
  link: (identity: string, trackId: TrackId) => void;
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
  // Length-prefix the title so the (title, artist) split is unambiguous: a
  // plain-space join lets distinct pairs like ("Encore", "Jay Z Interlude") and
  // ("Encore Jay Z", "Interlude") collide onto one key. The leading title length
  // pins the boundary regardless of the characters either field contains.
  return `${t.length}:${t}:${a}`;
}

export function linkTrackIdentity(identity: string | null, trackId: TrackId): void {
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
    return s.identities[identity];
  });
}

export function patchTrackStatus(trackId: TrackId, status: TrackStatus): void {
  useTrackStatusStore.getState().patch(trackId, status);
}

export function removeTrackStatus(trackId: TrackId): void {
  useTrackStatusStore.getState().remove(trackId);
}

export function useTrackStatus(trackId: TrackId | null): TrackStatus | undefined {
  return useTrackStatusStore((s) => (trackId === null ? undefined : s.statuses[trackId]));
}
