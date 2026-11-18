import type { Config } from '@jest/types';

const EVERY_TIMER_BUT_DATE: Config.FakeableAPI[] = [
  'hrtime',
  'nextTick',
  'performance',
  'queueMicrotask',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'requestIdleCallback',
  'cancelIdleCallback',
  'setImmediate',
  'clearImmediate',
  'setInterval',
  'clearInterval',
  'setTimeout',
  'clearTimeout',
];

export type FrozenDate = { advance(ms: number): void };

export function freezeDate(now: number): FrozenDate {
  jest.useFakeTimers({ doNotFake: EVERY_TIMER_BUT_DATE, now });
  return { advance: (ms) => jest.setSystemTime(Date.now() + ms) };
}
