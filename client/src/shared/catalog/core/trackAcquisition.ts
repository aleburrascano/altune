import type {
  AcquisitionStatus,
  FailedAcquisition,
  PendingAcquisition,
  ReadyAcquisition,
  TrackAcquisition,
} from '@shared/catalog/core/trackResponse';

type MaybeStatus = AcquisitionStatus | string | null | undefined;

export function isReady<S extends MaybeStatus>(status: S): status is S & 'ready' {
  return status === 'ready';
}

export function isPending<S extends MaybeStatus>(status: S): status is S & 'pending' {
  return status === 'pending';
}

export function isFailed<S extends MaybeStatus>(status: S): status is S & 'failed' {
  return status === 'failed';
}

type CarriesStatus = { acquisition_status: AcquisitionStatus };
type CarriesFailedStatus = { acquisition_status: 'failed' };

export function isFailedTrack<T extends CarriesStatus>(
  track: T,
): track is Extract<T, CarriesFailedStatus> {
  return isFailed(track.acquisition_status);
}

export function canRetry<S extends MaybeStatus>(status: S): status is S & 'failed' {
  return isFailed(status);
}

export type AcquisitionTransition =
  Required<PendingAcquisition> | Required<ReadyAcquisition> | Required<FailedAcquisition>;

type TrackStatusOf<T extends AcquisitionTransition> = T extends unknown
  ? { acquisitionStatus: T['acquisition_status']; failureMessage: T['failure_message'] }
  : never;

export type WireTrackStatus = TrackStatusOf<AcquisitionTransition>;

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

export function toTrackStatus(transition: AcquisitionTransition): WireTrackStatus {
  switch (transition.acquisition_status) {
    case 'failed':
      return { acquisitionStatus: 'failed', failureMessage: transition.failure_message };
    case 'pending':
      return { acquisitionStatus: 'pending', failureMessage: null };
    case 'ready':
      return { acquisitionStatus: 'ready', failureMessage: null };
  }
}

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
