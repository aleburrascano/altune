import type {
  FailedAcquisition,
  PendingAcquisition,
  ReadyAcquisition,
  TrackAcquisition,
} from './types';

// The whole status/reason/message triple with failure_message present, so
// spreading one over a cached track replaces any failure text instead of
// shallow-merging around it.
export type AcquisitionTransition =
  | Required<PendingAcquisition>
  | Required<ReadyAcquisition>
  | Required<FailedAcquisition>;

// The only constructors for an acquisition state change. Every transition site
// (SSE handlers, retry, reacquire) goes through these rather than hand-writing a
// partial patch that forgets to null the failure fields.

export function toPending(): Required<PendingAcquisition> {
  return { acquisition_status: 'pending', failure_reason: null, failure_message: null };
}

export function toReady(): Required<ReadyAcquisition> {
  return { acquisition_status: 'ready', failure_reason: null, failure_message: null };
}

export function toFailed(
  reason: string | null,
  message: string | null,
): Required<FailedAcquisition> {
  return { acquisition_status: 'failed', failure_reason: reason, failure_message: message };
}

// A track's current acquisition triple detached from its other fields, e.g. to
// roll an optimistic transition back or to let an incoming track's state win.
export function acquisitionOf(track: TrackAcquisition): AcquisitionTransition {
  switch (track.acquisition_status) {
    case 'failed':
      return toFailed(track.failure_reason, track.failure_message ?? null);
    case 'pending':
      return toPending();
    case 'ready':
      return toReady();
  }
}
