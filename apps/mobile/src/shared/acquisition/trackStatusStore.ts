import { create } from 'zustand';

import type { TrackId } from '@shared/api-client/ids';
import type { TrackStatus } from '@shared/api-client/trackAcquisition';
import { onSignOut } from '@shared/session/signOutCleanup';
import { recordStatusChanged, type StatusChangeSource } from '@shared/acquisition/acquisitionTelemetry';

// Built only by `toTrackStatus`, beside the transition constructors it mirrors,
// so the legal status/message pairings have one owner for both sides.
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

// Nothing but sign-out clears these maps, so a session that saves or imports
// thousands of tracks grows them — and the copy-on-write `patch` pays per event —
// without bound (#1789). A `ready` track has settled: the library row it belongs
// to already carries that status in the query cache, so its entry is still
// load-bearing only for a never-saved search row resolving through the identity
// link. This keeps more than any screen can show (two full library pages) and
// evicts the ready entries older than that.
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
  // A track already holding a ready status keeps the eviction slot it has, so a
  // replayed completion cannot spend a second one and evict the track itself.
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

// Statuses and identity links are one account's view of one account's library;
// an entry left behind would answer for a trackId the next identity cannot see.
onSignOut(() => useTrackStatusStore.getState().reset());

// Pinned, never the device's own locale: an optimistic download and the
// `track_added_to_library` event it must reconcile with are linked by this key
// alone, and a Turkish-locale device that folds `İ`/`I` its own way mints a key
// no other device — and no earlier session — would produce (#1778).
const IDENTITY_FOLD_LOCALE = 'en-US';

export function trackIdentityKey(title: string, artist: string): string | null {
  const foldedTitle = title.trim().toLocaleLowerCase(IDENTITY_FOLD_LOCALE);
  const foldedArtist = artist.trim().toLocaleLowerCase(IDENTITY_FOLD_LOCALE);
  if (foldedTitle.length === 0 || foldedArtist.length === 0) return null;
  // Length-prefix the title so the (title, artist) split is unambiguous: a
  // plain-space join lets distinct pairs like ("Encore", "Jay Z Interlude") and
  // ("Encore Jay Z", "Interlude") collide onto one key. The leading title length
  // pins the boundary regardless of the characters either field contains.
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
  if (prior !== status.acquisitionStatus) recordStatusChanged(trackId, prior, status.acquisitionStatus, source);
}

/** True when the store has already settled this track at `ready`. */
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
