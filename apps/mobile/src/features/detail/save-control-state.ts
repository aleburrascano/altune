import type { TrackResponse } from '@shared/api-client/types';

export type SaveControlState = 'add' | 'saving' | 'ready' | 'failed';

export function saveControlState(match: TrackResponse | null): SaveControlState {
  if (match === null) {
    return 'add';
  }
  if (match.acquisition_status === 'failed') {
    return 'failed';
  }
  if (match.acquisition_status === 'pending') {
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
