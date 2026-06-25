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
