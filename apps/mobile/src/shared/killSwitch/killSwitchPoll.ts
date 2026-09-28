import { AppState } from 'react-native';

import { startDeadline } from '@shared/deadline/deadline';

import { applyKillSwitches } from './killSwitch';

export const DEFAULT_KILL_SWITCH_URL =
  'https://raw.githubusercontent.com/aleburrascano/altune/main/kill-switches.json';

export const KILL_SWITCH_TIMEOUT_MS = 5_000;
export const KILL_SWITCH_POLL_MS = 5 * 60 * 1000;

function killSwitchUrl(): string {
  const configured = process.env.EXPO_PUBLIC_KILL_SWITCH_URL;
  return configured == null || configured === '' ? DEFAULT_KILL_SWITCH_URL : configured;
}

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
