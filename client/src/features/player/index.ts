import { PlaybackProvider } from '@features/player/view/PlaybackProvider';

export { PlaybackProvider };
export { SleepTimerBridge } from '@features/player/view/SleepTimerBridge';
export { MiniPlayer } from '@features/player/ui/MiniPlayer';
export { PlayerBar } from '@features/player/ui/PlayerBar';
export { FullPlayer } from '@features/player/ui/FullPlayer';
export { LyricsSheet } from '@features/player/ui/LyricsSheet';
export { QueueSheet, QueueRow } from '@features/player/ui/QueueSheet';
export {
  createStartPlaybackService,
  startPlaybackService,
} from '@features/player/adapters/startPlaybackService';
export type { StartPlaybackServiceDeps } from '@features/player/adapters/startPlaybackService';
export { httpPlaybackPort } from '@features/player/adapters/http/httpPlaybackPort';
export { installPlaybackPort, playbackPort } from '@features/player/ports/activePlaybackPort';
export {
  MAX_PREFETCH_FILE_BYTES,
  buildCacheFileName,
  cacheDir,
  evict,
  evictAllCached,
  evictCachedFile,
  findCached,
} from '@features/player/adapters/native/audioCache';
export type { PlaybackPort } from '@features/player/ports/playbackPort';
export { installArtworkPlaceholder } from '@features/player/ports/artworkPlaceholder';
export { evictCached, prefetchNext } from '@features/player/adapters/native/audioPrefetch';
export {
  forgetAllSwaps,
  repairActiveToStreaming,
  swapUpcomingToLocal,
  wasSwappedToLocal,
} from '@features/player/adapters/native/nativeTrackSwap';
export {
  PLAYBACK_HEALTH_BATCH,
  _resetPlaybackHealthForTest,
  flushPlaybackHealth,
  recordPresignOutcome,
  recordQueueRestoreFailure,
  recordQueueSaveFailure,
  recordQueueStateRejected,
} from '@features/player/domain/playbackHealth';
export {
  createNativePlaybackActions,
  ignoringNativeRejection,
} from '@features/player/adapters/native/createNativePlaybackActions';
export {
  createEnsurePlayerSetup,
  ensurePlayerSetup,
} from '@features/player/adapters/native/initPlayer';
export type { SetupPlayer } from '@features/player/adapters/native/initPlayer';
export {
  NativeQueueTimeoutError,
  onNativeQueueTimeout,
  withNativeQueue,
} from '@features/player/native/nativeQueueLock';
export {
  clearPlaybackError,
  reportLoadFailure,
  reportPlaybackError,
  usePlaybackErrorStore,
} from '@features/player/core/playbackErrorStore';
export { usePlaybackErrorFor } from '@features/player/view/usePlaybackErrorFor';
export {
  isIndexOutOfBounds,
  reportQueueFailure,
  reportingQueueFailure,
} from '@features/player/adapters/native/queueFailureReport';
export { derivePlaybackState } from '@features/player/domain/derivePlaybackState';
export type { DerivePlaybackStateInput } from '@features/player/domain/derivePlaybackState';
export type { RedactedPlaybackFailure } from '@features/player/domain/playbackError';
export {
  appendNativeTrack,
  insertNativeTrackNext,
  loadNativeQueue,
  loadNativeTrack,
  refreshUpcomingPresign,
} from '@features/player/adapters/native/loadNativeTrack';
export { reorderUpcomingNative } from '@features/player/adapters/native/rebuildNativeTail';
export { claimLoad } from '@features/player/domain/loadToken';
export { activeNativeTrackId, toNativeTrack } from '@features/player/adapters/native/nativeTrack';
export { NATIVE_QUEUE_WINDOW, markPresignedFrom } from '@features/player/domain/presignWindow';
export {
  createPlaybackService,
  playbackService,
  registerPlaybackService,
  resetPlaybackForSignOut,
} from '@features/player/adapters/native/playbackService';
export type { PlaybackServiceDeps } from '@features/player/adapters/native/playbackService';
export { _lyricsView, activeLineIndex } from '@features/player/domain/lyricsSync';
export {
  beginNativeLoad,
  endNativeLoad,
  shouldApplyActiveIndex,
} from '@features/player/native/nativeSyncGuard';
export {
  classifyNativePlaybackError,
  classifyPlaybackFailure,
  redactPlaybackErrorMessage,
  warnPlayback,
} from '@features/player/domain/playbackError';
export type { QueueRestoreStage } from '@features/player/domain/playbackHealth';
export {
  PLAYBACK_RATES,
  usePlaybackRateStore,
  useSpeedOptions,
} from '@features/player/view/usePlaybackRate';
export { useSleepOptions, useSleepTimerStore } from '@features/player/view/useSleepTimer';
export { PlayerOptionsSheets } from '@features/player/view/PlayerOptionsSheets';
export {
  canRetryPlaybackError,
  endedLabel,
  libraryTrackId,
  statusFlagsOf,
} from '@features/player/domain/playerStatus';
export {
  markPlayRequested,
  notePlaybackState,
  resetPlayStartTiming,
} from '@features/player/adapters/native/playStartTiming';
export { isExpoGo } from '@features/player/domain/isExpoGo';
export { playsThroughTrackPlayer } from '@features/player/domain/playsThroughTrackPlayer';
export {
  currentOccurrence,
  currentTrackId,
  isSavedCurrentResumable,
  rebuildOnFirstWorkingRungReportingCurrent,
  reconstructPlayOrder,
  resolveResumeStartIndex,
  showSavedTrackWhileRehydrating,
} from '@features/player/domain/queueResume';
export { savedQueueLists } from '@features/player/domain/queueSaveWindow';
export {
  asRepeatMode,
  fromWireSource,
  parseQueueState,
  toWireSource,
} from '@features/player/domain/queueStateWire';
export { claimRecoveryAttempt, resetRecoveryBudget } from '@features/player/native/recoveryBudget';
export { buildTrackPayload, hasCrossedListenThreshold } from '@features/player/domain/signals';
export { TrackPlayerPlaybackProvider } from '@features/player/view/native/TrackPlayerPlaybackProvider';
export { NativePlaybackProvider } from '@features/player/view/NativePlaybackProvider';
export { trackPlayerPlayback } from '@features/player/adapters/native/trackPlayerPlayback';
export type { NativePlayback } from '@features/player/ports/nativePlayback';
export type { WidgetBridge } from '@features/player/ports/widgetBridge';
export {
  msToSeconds,
  progressSecondsToMs,
  progressSecondsToRoundedMs,
  secondsToMs,
} from '@features/player/domain/time';
export {
  buildUpNextItems,
  formatSeconds,
  queueMenuOptions,
  queueSourceLabel,
} from '@features/player/domain/upNextQueue';
export type { QueueItem, QueueMenuContext } from '@features/player/domain/upNextQueue';
export { useMediaSession } from '@features/player/view/useMediaSession';
export { usePlaybackPosition } from '@features/player/view/usePlaybackPosition';
export { usePlaybackSignals } from '@features/player/view/usePlaybackSignals';
export { useQueueResume } from '@features/player/view/useQueueResume';
export { ExpoGoPlaybackProvider } from '@features/player/view/native/ExpoGoPlaybackProvider';
export { startWidgetCommands } from '@features/player/adapters/native/widgetCommands';
export {
  createWidgetSnapshotPublisher,
  startWidgetSnapshotPublisher,
} from '@features/player/adapters/native/widgetSnapshot';
export { Scrubber } from '@features/player/view/Scrubber';
export {
  SheetHeader,
  SheetHeaderCenter,
  SheetHeaderTrailing,
  SheetScreen,
} from '@features/player/view/SheetHeader';
export {
  bindAudioVolume,
  toVolume,
  usePlaybackVolumeStore,
} from '@features/player/web/playbackVolume';
export { VolumeSlider } from '@features/player/view/web/VolumeSlider';
export { WebPlaybackProvider } from '@features/player/view/web/WebPlaybackProvider';
export { resolveSource } from '@features/player/web/webPresign';
