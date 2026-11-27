export { useSharedRetryAcquisition } from '@shared/acquisition/hooks/track/useRetryAcquisition';
export {
  recordFailureShownOnce,
  recordRetryRequest,
  recordRetryTapped,
  _resetAcquisitionTelemetryForTest,
  recordStatusChanged,
  retryFailureOutcome,
} from '@shared/acquisition/state/acquisitionTelemetry';
export type {
  RetryEntryPoint,
  RetrySkipReason,
} from '@shared/acquisition/state/acquisitionTelemetry';
export {
  invalidateAudioCaches,
  registerAudioCacheInvalidator,
  _resetAudioCacheInvalidatorsForTest,
} from '@shared/acquisition/state/audioCacheInvalidation';
export {
  completeDownload,
  failDownload,
  isStaleStart,
  progressDownload,
  rememberDownloadMeta,
  startDownload,
  useActiveDownloadItems,
  useDownloadPhase,
  aggregatePhase,
  useDownloadStore,
} from '@shared/acquisition/view/downloadStore';
export type {
  DownloadMeta,
  DownloadPhase,
  DownloadEntry,
} from '@shared/acquisition/view/downloadStore';
export { phaseLabel, stageToPhase, ACQUISITION_PHASES } from '@shared/acquisition/state/stagePhase';
export {
  adoptResponseStatus,
  failedTrackStatus,
  isTrackStatusReady,
  linkTrackIdentity,
  patchTrackStatus,
  removeTrackStatus,
  trackIdForIdentityAtCallTime,
  trackIdentityKey,
  trackStatusAtCallTime,
  useTrackIdForIdentity,
  useTrackStatus,
  useTrackStatusStore,
} from '@shared/acquisition/view/trackStatusStore';
export type { TrackStatus } from '@shared/acquisition/view/trackStatusStore';
export { nextLifecycle } from '@shared/acquisition/core/acquisitionLifecycle';
export type { LifecycleEvent, LifecycleState } from '@shared/acquisition/core/acquisitionLifecycle';
export { DownloadsBar } from '@shared/acquisition/view/DownloadsBar';
export { DownloadsSheet } from '@shared/acquisition/view/DownloadsSheet';
export {
  AcquisitionPortProvider,
  useAcquisitionPort,
} from '@shared/acquisition/view/AcquisitionPortProvider';
export type { AcquisitionPort } from '@shared/acquisition/ports/acquisitionPort';
export type { AcquisitionPhase } from '@shared/acquisition/state/stagePhase';
export { deriveBarDisplay } from '@shared/acquisition/ui/barDisplay';
