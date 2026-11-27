import { setTimeout as nextTimerTick } from 'node:timers/promises';

export async function flushPromises(): Promise<void> {
  await nextTimerTick(0);
}
