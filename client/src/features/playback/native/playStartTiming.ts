import { State } from 'react-native-track-player';

import { isTelemetryGated } from '@shared/failure/errors';
import { recordEvent } from '@shared/telemetry/recordEvent';

import { warnPlayback } from '@features/playback/domain/playbackError';

export type PlayTrigger = 'play' | 'queue' | 'resume' | 'remote';

const GIVE_UP_MS = 60_000;

interface PendingStart {
  trigger: PlayTrigger;
  requestedAt: number;
  cold: boolean;
  giveUp: ReturnType<typeof setTimeout>;
}

let pending: PendingStart | null = null;
let isAudible = false;
let hasBeenAudible = false;

const LOADS_NEW_AUDIO: ReadonlySet<PlayTrigger> = new Set(['play', 'queue']);

function changesWhatIsHeard(trigger: PlayTrigger): boolean {
  return !isAudible || LOADS_NEW_AUDIO.has(trigger);
}

export function markPlayRequested(trigger: PlayTrigger): void {
  const isRetryWhileSilent = pending !== null;
  if (isRetryWhileSilent || !changesWhatIsHeard(trigger)) return;
  pending = {
    trigger,
    requestedAt: Date.now(),
    cold: !hasBeenAudible,
    giveUp: setTimeout(() => settle('never_started'), GIVE_UP_MS),
  };
}

export function notePlaybackState(state: State | undefined): void {
  isAudible = state === State.Playing;
  if (isAudible) settle('playing');
}

function settle(outcome: 'playing' | 'never_started'): void {
  const start = pending;
  if (start === null) return;
  pending = null;
  clearTimeout(start.giveUp);
  if (outcome === 'playing') hasBeenAudible = true;
  recordEvent({
    type: 'play_start',
    payload: {
      outcome,
      trigger: start.trigger,
      cold: start.cold,
      start_ms: Date.now() - start.requestedAt,
    },
  }).catch((err: unknown) => {
    if (isTelemetryGated(err)) return;
    warnPlayback('failed to record play start', {}, err);
  });
}

export function resetPlayStartTiming(): void {
  if (pending !== null) clearTimeout(pending.giveUp);
  pending = null;
  isAudible = false;
  hasBeenAudible = false;
}
