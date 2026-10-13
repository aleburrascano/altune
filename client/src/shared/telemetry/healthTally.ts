import { subscribeAppState } from '@shared/lifecycle/appState';
import { recordEvent, type DiscoveryEventType } from '@shared/telemetry/recordEvent';

export const HEALTH_BATCH = 25;

type HealthTallyOptions<K extends string> = {
  eventType: DiscoveryEventType;
  keys: readonly K[];
  onFlushFailure: (error: unknown) => void;
};

type HealthTally<K extends string> = {
  count: (key: K) => void;
  flush: () => void;
  reset: () => void;
};

type TallyState<K extends string> = {
  tally: Record<K, number>;
  outcomes: number;
  listening: boolean;
};

function emptyTally<K extends string>(keys: readonly K[]): Record<K, number> {
  return Object.fromEntries(keys.map((key) => [key, 0])) as Record<K, number>;
}

function resetState<K extends string>(state: TallyState<K>, keys: readonly K[]): void {
  state.tally = emptyTally(keys);
  state.outcomes = 0;
}

function flushState<K extends string>(
  state: TallyState<K>,
  { eventType, keys, onFlushFailure }: HealthTallyOptions<K>,
): void {
  if (state.outcomes === 0) return;
  const payload = state.tally;
  resetState(state, keys);
  recordEvent({ type: eventType, payload }).catch(onFlushFailure);
}

function flushOnBackground<K extends string>(
  state: TallyState<K>,
  options: HealthTallyOptions<K>,
): void {
  if (state.listening) return;
  state.listening = true;
  subscribeAppState((status) => {
    if (status === 'background') flushState(state, options);
  });
}

function countOutcome<K extends string>(
  state: TallyState<K>,
  options: HealthTallyOptions<K>,
  key: K,
): void {
  flushOnBackground(state, options);
  state.tally[key] += 1;
  state.outcomes += 1;
  if (state.outcomes >= HEALTH_BATCH) flushState(state, options);
}

function newState<K extends string>(keys: readonly K[]): TallyState<K> {
  return { tally: emptyTally(keys), outcomes: 0, listening: false };
}

export function createHealthTally<K extends string>(
  options: HealthTallyOptions<K>,
): HealthTally<K> {
  const state = newState(options.keys);
  return {
    count: (key) => countOutcome(state, options, key),
    flush: () => flushState(state, options),
    reset: () => resetState(state, options.keys),
  };
}
