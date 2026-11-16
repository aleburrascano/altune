import { applyKillSwitches } from '@shared/killSwitch';
import { subscribeAppState } from '@shared/lifecycle';
import { startDeadline } from '@shared/timing';

const DEFAULT_KILL_SWITCH_URL =
  'https://raw.githubusercontent.com/aleburrascano/altune/main/kill-switches.json';

const KILL_SWITCH_TIMEOUT_MS = 5_000;
const KILL_SWITCH_POLL_MS = 300_000;

type Poll = { interval: ReturnType<typeof setInterval> | undefined };

function killSwitchUrl(): string {
  const configured = process.env.EXPO_PUBLIC_KILL_SWITCH_URL;
  return configured == null || configured === '' ? DEFAULT_KILL_SWITCH_URL : configured;
}

async function fetchSwitches(signal: AbortSignal): Promise<void> {
  const response = await fetch(killSwitchUrl(), { signal, cache: 'no-store' });
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  applyKillSwitches(await response.json());
}

async function refreshKillSwitches(): Promise<void> {
  const deadline = startDeadline(undefined, KILL_SWITCH_TIMEOUT_MS);
  try {
    await fetchSwitches(deadline.signal);
  } catch (error) {
    console.warn('[kill-switch] refresh failed; keeping the current switches', error);
  } finally {
    deadline.release();
  }
}

function startInterval(poll: Poll): void {
  poll.interval ??= setInterval(() => void refreshKillSwitches(), KILL_SWITCH_POLL_MS);
}

function stopInterval(poll: Poll): void {
  clearInterval(poll.interval);
  poll.interval = undefined;
}

function followAppState(poll: Poll): (state: string) => void {
  return (state) => {
    if (state !== 'active') return stopInterval(poll);
    void refreshKillSwitches();
    startInterval(poll);
  };
}

export function startKillSwitchPolling(onAppStateChange = subscribeAppState): () => void {
  const poll: Poll = { interval: undefined };
  void refreshKillSwitches();
  startInterval(poll);
  const unsubscribeAppState = onAppStateChange(followAppState(poll));
  return () => {
    unsubscribeAppState();
    stopInterval(poll);
  };
}
