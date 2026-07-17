/**
 * saveControlState — maps a library-cache match to the save/download lifecycle
 * state the row's save control renders.
 *
 * The lifecycle is driven entirely by the library cache: a freshly saved track
 * is inserted optimistically as `pending` (downloading), then reconciles to
 * `ready` (or `failed`) when acquisition settles. No match means the track is
 * not in the library yet, so the control offers "add". Pure — unit-tested
 * without rendering.
 */

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

/**
 * The lifecycle's accessibility label, shared by the hero save pill and the
 * row control so the vocabulary can't drift between them.
 */
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

/** The lifecycle's visible caption (hero save pill). */
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
