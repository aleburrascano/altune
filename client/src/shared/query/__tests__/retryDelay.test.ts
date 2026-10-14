import fc from 'fast-check';

import * as retryDelayModule from '@shared/query/retryDelay';
import { transientRetryOptions } from '@shared/query/retryDelay';

const RETRY_BACKOFF_BASE_MS = 1_000;
const RETRY_BACKOFF_CAP_MS = 30_000;

const ATTEMPTS_IN_A_SESSION = [0, 1, 2, 3, 4];
const DOUBLING = 2;
const ATTEMPTS_PAST_THE_CAP = 10;
const SAMPLES_PER_ATTEMPT = 20;

function retryDelayMs(failureCount: number, random: number): number {
  const sample = jest.spyOn(Math, 'random').mockReturnValue(random);
  const delay = transientRetryOptions.retryDelay(failureCount);
  sample.mockRestore();
  return delay;
}

function scheduleForOneClient(): number[] {
  return ATTEMPTS_IN_A_SESSION.map((failureCount) => retryDelayMs(failureCount, Math.random()));
}

describe('jitter: clients that fail together do not retry together', () => {
  it('spreads the wait for one attempt number across many values instead of one', () => {
    const waits = new Set(Array.from({ length: 100 }, () => retryDelayMs(3, Math.random())));

    expect(waits.size).toBeGreaterThan(1);
  });

  it('gives two clients different retry schedules for the same run of failures', () => {
    const [first, second] = [scheduleForOneClient(), scheduleForOneClient()];

    expect(first).not.toEqual(second);
  });
});

describe('law: retryDelayMs is exponential, jittered into [ceiling/2, ceiling], and capped', () => {
  it('starts at the base ceiling and doubles per failed attempt', () => {
    expect(retryDelayMs(0, 0)).toBe(RETRY_BACKOFF_BASE_MS / 2);
    expect(retryDelayMs(0, 1)).toBe(RETRY_BACKOFF_BASE_MS);
    expect(retryDelayMs(2, 1)).toBe(RETRY_BACKOFF_BASE_MS * 4);
  });

  it('treats a negative failure count as the first attempt', () => {
    expect(retryDelayMs(-1, 1)).toBe(RETRY_BACKOFF_BASE_MS);
  });

  it('never exceeds the cap however many attempts have failed', () => {
    for (const failureCount of [20, 31, 1000]) {
      expect(retryDelayMs(failureCount, 0)).toBe(RETRY_BACKOFF_CAP_MS / 2);
      expect(retryDelayMs(failureCount, 0.999999)).toBeLessThanOrEqual(RETRY_BACKOFF_CAP_MS);
    }
  });

  it('holds for any attempt number and any sample in [0, 1)', () => {
    fc.assert(
      fc.property(
        fc.integer({ min: -5, max: 60 }),
        fc.double({ min: 0, max: 1, maxExcluded: true, noNaN: true }),
        (failureCount, random) => {
          const ceiling = retryDelayMs(failureCount, 1);
          const nextCeiling = retryDelayMs(failureCount + 1, 1);

          const delay = retryDelayMs(failureCount, random);

          expect(delay).toBeGreaterThanOrEqual(ceiling / 2);
          expect(delay).toBeLessThanOrEqual(ceiling);
          expect(delay).toBeLessThanOrEqual(RETRY_BACKOFF_CAP_MS);
          if (failureCount >= 0) {
            expect(nextCeiling).toBe(Math.min(ceiling * DOUBLING, RETRY_BACKOFF_CAP_MS));
          } else {
            expect(ceiling).toBe(RETRY_BACKOFF_BASE_MS);
          }
        },
      ),
    );
  });
});

describe('the app-wide QueryClient retries on the jittered schedule', () => {
  it('waits within the jitter band of retryDelayMs for each failed attempt', () => {
    for (const failureCount of [...ATTEMPTS_IN_A_SESSION, ATTEMPTS_PAST_THE_CAP]) {
      for (let sample = 0; sample < SAMPLES_PER_ATTEMPT; sample += 1) {
        const delay = transientRetryOptions.retryDelay(failureCount);

        expect(delay).toBeGreaterThanOrEqual(retryDelayMs(failureCount, 0));
        expect(delay).toBeLessThanOrEqual(retryDelayMs(failureCount, 1));
      }
    }
  });
});

describe('retryDelay module surface', () => {
  it('exposes only the retry options, keeping its backoff internals private', () => {
    expect(Object.keys(retryDelayModule)).toEqual(['transientRetryOptions']);
  });
});
