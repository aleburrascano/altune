import { create } from 'zustand';

import type { TrackId } from '@shared/api-client/ids';
import type { TrackStatus } from '@shared/api-client/trackAcquisition';
import { onSignOut } from '@shared/session/signOutCleanup';
import {
  recordStatusChanged,
  type StatusChangeSource,
} from '@shared/acquisition/acquisitionTelemetry';

export type { TrackStatus };

type TrackStatusState = {
  statuses: Record<string, TrackStatus>;
  identities: Record<string, TrackId>;
  readyTrackIds: TrackId[];
  patch: (trackId: TrackId, status: TrackStatus) => void;
  remove: (trackId: TrackId) => void;
  link: (identity: string, trackId: TrackId) => void;
  reset: () => void;
};

type TrackStatusEntries = Pick<TrackStatusState, 'statuses' | 'identities' | 'readyTrackIds'>;

export const READY_STATUS_LIMIT = 512;

function isReady(status: TrackStatus | undefined): boolean {
  return status?.acquisitionStatus === 'ready';
}

function withoutKeys<T>(entries: Record<string, T>, drop: ReadonlySet<string>): Record<string, T> {
  return Object.fromEntries(Object.entries(entries).filter(([key]) => !drop.has(key)));
}

function withoutLinksTo(
  identities: Record<string, TrackId>,
  drop: ReadonlySet<string>,
): Record<string, TrackId> {
  return Object.fromEntries(Object.entries(identities).filter(([, linked]) => !drop.has(linked)));
}

function prunedToReadyLimit(entries: TrackStatusEntries): TrackStatusEntries {
  const overflow = entries.readyTrackIds.length - READY_STATUS_LIMIT;
  if (overflow <= 0) return entries;
  const evicted = new Set<string>(entries.readyTrackIds.slice(0, overflow));
  return {
    statuses: withoutKeys(entries.statuses, evicted),
    identities: withoutLinksTo(entries.identities, evicted),
    readyTrackIds: entries.readyTrackIds.slice(overflow),
  };
}

export const useTrackStatusStore = create<TrackStatusState>((set) => ({
  statuses: {},
  identities: {},
  readyTrackIds: [],
  patch: (trackId, status) =>
    set((s) => {
      const statuses = { ...s.statuses, [trackId]: status };
      if (!isReady(status) || isReady(s.statuses[trackId])) return { statuses };
      return prunedToReadyLimit({
        statuses,
        identities: s.identities,
        readyTrackIds: [...s.readyTrackIds, trackId],
      });
    }),
  remove: (trackId) =>
    set((s) => {
      if (!(trackId in s.statuses)) return s;
      return {
        statuses: withoutKeys(s.statuses, new Set<string>([trackId])),
        readyTrackIds: s.readyTrackIds.filter((id) => id !== trackId),
      };
    }),
  link: (identity, trackId) =>
    set((s) => ({ identities: { ...s.identities, [identity]: trackId } })),
  reset: () => set({ statuses: {}, identities: {}, readyTrackIds: [] }),
}));

onSignOut(() => useTrackStatusStore.getState().reset());

const IDENTITY_FOLD_LOCALE = 'en-US';

export function trackIdentityKey(title: string, artist: string): string | null {
  const foldedTitle = title.trim().toLocaleLowerCase(IDENTITY_FOLD_LOCALE);
  const foldedArtist = artist.trim().toLocaleLowerCase(IDENTITY_FOLD_LOCALE);
  if (foldedTitle.length === 0 || foldedArtist.length === 0) return null;
  return `${foldedTitle.length}:${foldedTitle}:${foldedArtist}`;
}

export function linkTrackIdentity(identity: string | null, trackId: TrackId): void {
  if (identity === null) return;
  useTrackStatusStore.getState().link(identity, trackId);
}

export function useTrackIdForIdentity(identity: string | null): TrackId | undefined {
  return useTrackStatusStore((s) => {
    if (identity === null) return undefined;
    return s.identities[identity];
  });
}

export function trackIdForIdentityAtCallTime(identity: string | null): TrackId | undefined {
  if (identity === null) return undefined;
  return useTrackStatusStore.getState().identities[identity];
}

function priorAcquisitionStatus(trackId: TrackId): TrackStatus['acquisitionStatus'] | null {
  return useTrackStatusStore.getState().statuses[trackId]?.acquisitionStatus ?? null;
}

export function patchTrackStatus(
  trackId: TrackId,
  status: TrackStatus,
  source: StatusChangeSource = 'response',
): void {
  const prior = priorAcquisitionStatus(trackId);
  useTrackStatusStore.getState().patch(trackId, status);
  if (prior !== status.acquisitionStatus)
    recordStatusChanged(trackId, prior, status.acquisitionStatus, source);
}

export function isTrackStatusReady(trackId: TrackId): boolean {
  return isReady(useTrackStatusStore.getState().statuses[trackId]);
}

export function removeTrackStatus(trackId: TrackId): void {
  useTrackStatusStore.getState().remove(trackId);
}

export function useTrackStatus(trackId: TrackId | null): TrackStatus | undefined {
  return useTrackStatusStore((s) => (trackId === null ? undefined : s.statuses[trackId]));
}

export function trackStatusAtCallTime(trackId: TrackId | null): TrackStatus | undefined {
  return trackId === null ? undefined : useTrackStatusStore.getState().statuses[trackId];
}
