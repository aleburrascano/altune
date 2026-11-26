import { createEnsurePlayerSetup, type SetupPlayer } from '@features/player';
import { recordLogs } from '@testing/fakes/log/inMemoryLogger';

const PLAYER_SETUP_TIMEOUT_MS = 15_000;
const ATTEMPTS_AFTER_A_FAILURE = 2;

const STILL_PENDING = 'still pending';

interface FakePlayer {
  player: SetupPlayer;
  setupCalls: () => number;
  updateCalls: () => number;
  failNextSetup: (error: Error) => void;
  hangNextSetup: () => void;
}

function fakePlayer(): FakePlayer {
  let setups = 0;
  let updates = 0;
  let nextFailure: Error | null = null;
  let shouldHang = false;
  const player: SetupPlayer = {
    setupPlayer: () => {
      setups += 1;
      if (shouldHang) {
        shouldHang = false;
        return new Promise<void>(() => undefined);
      }
      if (nextFailure) {
        const failure = nextFailure;
        nextFailure = null;
        return Promise.reject(failure);
      }
      return Promise.resolve();
    },
    updateOptions: () => {
      updates += 1;
      return Promise.resolve();
    },
  };
  return {
    player,
    setupCalls: () => setups,
    updateCalls: () => updates,
    failNextSetup: (error) => {
      nextFailure = error;
    },
    hangNextSetup: () => {
      shouldHang = true;
    },
  };
}

function settlementOf(promise: Promise<void>): () => unknown {
  let settlement: unknown = STILL_PENDING;
  void promise.then(
    () => {
      settlement = 'resolved';
    },
    (err: unknown) => {
      settlement = err;
    },
  );
  return () => settlement;
}

describe('ensurePlayerSetup', () => {
  it('sets the player up only once across repeated successful calls', async () => {
    const native = fakePlayer();
    const ensurePlayerSetup = createEnsurePlayerSetup(native.player);

    await ensurePlayerSetup();
    await ensurePlayerSetup();

    expect(native.setupCalls()).toBe(1);
  });

  it('retries setup after a failed attempt instead of replaying the cached rejection', async () => {
    const native = fakePlayer();
    const ensurePlayerSetup = createEnsurePlayerSetup(native.player);
    native.failNextSetup(new Error('native module not ready'));

    await expect(ensurePlayerSetup()).rejects.toThrow('native module not ready');
    await expect(ensurePlayerSetup()).resolves.toBeUndefined();

    expect(native.setupCalls()).toBe(ATTEMPTS_AFTER_A_FAILURE);
    expect(native.updateCalls()).toBe(1);
  });

  it('does not warn about a setup that fails before its deadline', async () => {
    const logged = recordLogs();
    const native = fakePlayer();
    const ensurePlayerSetup = createEnsurePlayerSetup(native.player);
    native.failNextSetup(new Error('native module not ready'));

    await expect(ensurePlayerSetup()).rejects.toThrow('native module not ready');

    expect(logged.warnings).toEqual([]);
  });

  it('shares one in-flight attempt between concurrent callers', async () => {
    const native = fakePlayer();
    const ensurePlayerSetup = createEnsurePlayerSetup(native.player);

    await Promise.all([ensurePlayerSetup(), ensurePlayerSetup()]);

    expect(native.setupCalls()).toBe(1);
  });

  it('keeps separate setups for separate players', async () => {
    const first = fakePlayer();
    const second = fakePlayer();

    await createEnsurePlayerSetup(first.player)();
    await createEnsurePlayerSetup(second.player)();

    expect([first.setupCalls(), second.setupCalls()]).toEqual([1, 1]);
  });

  describe('when native setup never settles', () => {
    beforeEach(() => jest.useFakeTimers());
    afterEach(() => jest.useRealTimers());

    it('rejects the caller once setup outlives its budget instead of waiting forever', async () => {
      const native = fakePlayer();
      const ensurePlayerSetup = createEnsurePlayerSetup(native.player);
      native.hangNextSetup();

      const settlement = settlementOf(ensurePlayerSetup());

      await jest.advanceTimersByTimeAsync(PLAYER_SETUP_TIMEOUT_MS - 1);
      expect(settlement()).toBe(STILL_PENDING);

      await jest.advanceTimersByTimeAsync(1);
      expect(settlement()).toMatchObject({ name: 'PlayerSetupTimeoutError' });
    });

    it('starts a fresh attempt after a timed-out one instead of caching the hung call', async () => {
      const native = fakePlayer();
      const ensurePlayerSetup = createEnsurePlayerSetup(native.player);
      native.hangNextSetup();
      const timedOut = ensurePlayerSetup().catch((err: unknown) => err);
      await jest.advanceTimersByTimeAsync(PLAYER_SETUP_TIMEOUT_MS);
      await timedOut;

      await expect(ensurePlayerSetup()).resolves.toBeUndefined();

      expect(native.setupCalls()).toBe(ATTEMPTS_AFTER_A_FAILURE);
      expect(native.updateCalls()).toBe(1);
    });

    it('warns about a setup that fails after its deadline, with the error', async () => {
      const logged = recordLogs();
      const late = Promise.withResolvers<void>();
      const ensurePlayerSetup = createEnsurePlayerSetup({
        setupPlayer: () => late.promise,
        updateOptions: () => Promise.resolve(),
      });
      const timedOut = ensurePlayerSetup().catch((err: unknown) => err);
      await jest.advanceTimersByTimeAsync(PLAYER_SETUP_TIMEOUT_MS);
      await timedOut;

      late.reject(new Error('native setup finally failed'));
      await jest.advanceTimersByTimeAsync(0);

      expect(logged.warnings[0]?.[0]).toBe('[playback] player setup failed after its deadline');
      expect(logged.text()).toContain('native setup finally failed');
    });

    it('clears the deadline once setup settles in time', async () => {
      const ensurePlayerSetup = createEnsurePlayerSetup(fakePlayer().player);

      await ensurePlayerSetup();

      expect(jest.getTimerCount()).toBe(0);
    });
  });
});
