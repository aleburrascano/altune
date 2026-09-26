import type { TrackId } from '@shared/api-client/ids';

import type { OwnedTrack } from './hooks/useOwnedTrack';
import type { SaveFailure } from './hooks/useSaveTrack';
import { isOptimisticTrackId } from './save-cache';

// `failed` is a save worth re-attempting; `rejected` is one that was refused for
// good, so no control offers a retry for it.
export type SaveControlState = 'add' | 'saving' | 'ready' | 'failed' | 'rejected';

export type SaveState = SaveControlState | 'disabled';

export function saveControlInteractive(state: SaveState): boolean {
  return state === 'add' || state === 'failed';
}

export function saveDisplayState(state: SaveState): SaveControlState {
  return state === 'disabled' ? 'add' : state;
}

export function saveControlState(owned: OwnedTrack | null): SaveControlState {
  if (owned === null) {
    return 'add';
  }
  if (owned.acquisitionStatus === 'failed') {
    return 'failed';
  }
  if (owned.acquisitionStatus === 'pending') {
    return 'saving';
  }
  return 'ready';
}

export function saveControlLabel(state: SaveControlState, title: string): string {
  switch (state) {
    case 'saving':
      return `${title} downloading`;
    case 'ready':
      return `${title} in library`;
    case 'failed':
      return `Retry saving ${title}`;
    case 'rejected':
      return `Couldn't save ${title}`;
    default:
      return `Save ${title}`;
  }
}

export function saveControlText(state: SaveControlState): string {
  switch (state) {
    case 'saving':
      return 'Saving…';
    case 'ready':
      return 'Saved';
    case 'failed':
      return 'Retry';
    case 'rejected':
      return "Can't save";
    default:
      return 'Save';
  }
}

export function ownedRetryTrackId(owned: OwnedTrack | null): TrackId | null {
  if (owned === null || owned.acquisitionStatus !== 'failed') {
    return null;
  }
  return isOptimisticTrackId(owned.trackId) ? null : owned.trackId;
}

export function saveFailureBanner(failure: SaveFailure): string {
  const nextStep = failure.isRetryable ? 'Tap Retry.' : "Retrying won't help.";
  return `Couldn't save this track. ${nextStep} (${failure.message})`;
}
