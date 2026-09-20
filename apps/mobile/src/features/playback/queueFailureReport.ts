import { type TrackKey } from '@shared/playback/trackKey';

import { NativeQueueTimeoutError } from './nativeQueueLock';
import { type PlaybackErrorKind, reportPlaybackError } from './playbackErrorStore';
import { recordPlaybackFailure } from './playbackHealth';

export type NativeQueueFailureKind = 'transient' | 'permanent';

const PERMANENT_NATIVE_CODES: ReadonlySet<string> = new Set([
  'index_out_of_bounds',
  'no_current_item',
  'player_not_initialized',
  'invalid_track_object',
]);

export const QUEUE_UPDATE_FAILED_MESSAGE = "Couldn't update the queue. Tap retry to resync.";
export const QUEUE_OUT_OF_SYNC_MESSAGE = 'The queue fell out of sync. Tap retry to reload it.';

const QUEUE_FAILURE_REPORT: Record<
  NativeQueueFailureKind,
  { errorKind: PlaybackErrorKind; message: string }
> = {
  permanent: { errorKind: 'queue_out_of_sync', message: QUEUE_OUT_OF_SYNC_MESSAGE },
  transient: { errorKind: 'queue_update_failed', message: QUEUE_UPDATE_FAILED_MESSAGE },
};

export function nativeErrorCode(err: unknown): string | null {
  if (typeof err !== 'object' || err === null || !('code' in err)) return null;
  return typeof err.code === 'string' ? err.code : null;
}

export function classifyNativeQueueFailure(err: unknown): NativeQueueFailureKind {
  if (err instanceof NativeQueueTimeoutError) return 'transient';
  const code = nativeErrorCode(err);
  return code !== null && PERMANENT_NATIVE_CODES.has(code) ? 'permanent' : 'transient';
}

function warnQueueMutationFailed(op: string, kind: NativeQueueFailureKind, err: unknown): void {
  console.warn('[playback] native queue mutation failed', {
    op,
    kind,
    code: nativeErrorCode(err),
    error: err,
  });
}

export function reportQueueFailure(key: TrackKey | null, op: string, err: unknown): void {
  const kind = classifyNativeQueueFailure(err);
  const { errorKind, message } = QUEUE_FAILURE_REPORT[kind];
  warnQueueMutationFailed(op, kind, err);
  recordPlaybackFailure(errorKind);
  if (key === null) return;
  reportPlaybackError(key, errorKind, message);
}

function reportUnlessSupersededByLoad(
  keyAtCall: TrackKey | null,
  currentKey: TrackKey | null,
  op: string,
  err: unknown,
): void {
  const isStillCurrent = keyAtCall !== null && keyAtCall === currentKey;
  reportQueueFailure(isStillCurrent ? keyAtCall : null, op, err);
}

async function runRoutingRejection(
  run: () => Promise<unknown>,
  onRejection: (err: unknown) => void,
): Promise<void> {
  try {
    await run();
  } catch (err) {
    onRejection(err);
  }
}

export function reportingQueueFailure(
  getKey: () => TrackKey | null,
  op: string,
  run: () => Promise<unknown>,
): Promise<void> {
  const keyAtCall = getKey();
  return runRoutingRejection(run, (err) =>
    reportUnlessSupersededByLoad(keyAtCall, getKey(), op, err),
  );
}
