import * as fs from 'fs';
import * as path from 'path';

import fc from 'fast-check';

import { RETRY_BACKOFF_BASE_MS, RETRY_BACKOFF_CAP_MS, retryDelayMs } from '../retryDelay';

const ATTEMPTS_IN_A_SESSION = [0, 1, 2, 3, 4];

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
          const ceiling = Math.min(
            RETRY_BACKOFF_CAP_MS,
            RETRY_BACKOFF_BASE_MS * 2 ** Math.max(failureCount, 0),
          );

          const delay = retryDelayMs(failureCount, random);

          expect(delay).toBeGreaterThanOrEqual(ceiling / 2);
          expect(delay).toBeLessThanOrEqual(ceiling);
        },
      ),
    );
  });
});

describe('the app-wide QueryClient retries on the jittered schedule', () => {
  it('_layout.tsx spreads transientRetryOptions, which derives retryDelay from retryDelayMs and a fresh sample', () => {
    const layoutSource = fs.readFileSync(
      path.join(__dirname, '..', '..', '..', 'app', '_layout.tsx'),
      'utf8',
    );
    const helperSource = fs.readFileSync(path.join(__dirname, '..', 'retryDelay.ts'), 'utf8');

    expect(layoutSource).toMatch(/queries:\s*\{[^}]*\.\.\.transientRetryOptions/);
    expect(helperSource).toMatch(
      /retryDelay:\s*\([^)]*\)\s*=>\s*retryDelayMs\([^)]*Math\.random\(\)\)/,
    );
  });
});
