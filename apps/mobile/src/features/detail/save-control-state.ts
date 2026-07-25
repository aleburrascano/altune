import type { OwnedTrack } from './hooks/useOwnedTrack';

export type SaveControlState = 'add' | 'saving' | 'ready' | 'failed';

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
    default:
      return 'Save';
  }
}
