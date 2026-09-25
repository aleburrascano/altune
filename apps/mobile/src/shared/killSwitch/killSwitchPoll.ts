import { AppState } from 'react-native';

import { startDeadline } from '@shared/api-client/deadline';

import { applyKillSwitches } from './killSwitch';

// Where the switches are fetched from: a static JSON file, polled. By default the one at the root
// of this public repository (served like apps.json), so flipping a loop off is a one-line change
// merged to main and needs neither an app release nor the API, which may be what is misbehaving.
// A build can point elsewhere with EXPO_PUBLIC_KILL_SWITCH_URL.
export const DEFAULT_KILL_SWITCH_URL =
  'https://raw.githubusercontent.com/aleburrascano/altune/main/kill-switches.json';

/** How long one switch fetch may take before it is abandoned. */
export const KILL_SWITCH_TIMEOUT_MS = 5_000;
/** How often the switches are re-read while the app stays in the foreground. */
export const KILL_SWITCH_POLL_MS = 5 * 60 * 1000;

function killSwitchUrl(): string {
  const configured = process.env.EXPO_PUBLIC_KILL_SWITCH_URL;
  return configured == null || configured === '' ? DEFAULT_KILL_SWITCH_URL : configured;
}

/**
 * Fetches the switch document once and applies it. A failed fetch keeps the switches as they were
 * (including what was persisted), so an outage of the switch host changes nothing.
 */
export async function refreshKillSwitches(url: string = killSwitchUrl()): Promise<void> {
  const deadline = startDeadline(undefined, KILL_SWITCH_TIMEOUT_MS);
  try {
    const response = await fetch(url, { signal: deadline.signal, cache: 'no-store' });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    applyKillSwitches(await response.json());
  } catch (error) {
    console.warn('[kill-switch] refresh failed; keeping the current switches', error);
  } finally {
    deadline.release();
  }
}

/**
 * Refreshes the switches now, on every return to the foreground, and periodically while active.
 * Called once for the app's lifetime; returns the stop function, which removes the listener and
 * the timer.
 */
export function startKillSwitchPolling(): () => void {
  void refreshKillSwitches();
  let interval: ReturnType<typeof setInterval> | undefined;
  const startInterval = (): void => {
    interval ??= setInterval(() => void refreshKillSwitches(), KILL_SWITCH_POLL_MS);
  };
  const stopInterval = (): void => {
    clearInterval(interval);
    interval = undefined;
  };
  startInterval();
  const subscription = AppState.addEventListener('change', (state) => {
    if (state !== 'active') return stopInterval();
    void refreshKillSwitches();
    startInterval();
  });
  return () => {
    subscription.remove();
    stopInterval();
  };
}
