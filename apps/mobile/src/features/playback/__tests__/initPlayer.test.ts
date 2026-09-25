import type * as InitPlayer from '../initPlayer';

const trackPlayerDouble = jest.requireMock('react-native-track-player');
const { __player } = trackPlayerDouble;

type InitPlayerModule = typeof InitPlayer;

// initPlayer caches its setup attempt in module state, so each test loads its own copy.
// The copy hands back its own timeout class too, the one its rejections are built from.
function freshInitPlayer(): InitPlayerModule {
  let loaded: InitPlayerModule | undefined;
  jest.isolateModules(() => {
    jest.doMock('react-native-track-player', () => trackPlayerDouble);
    loaded = require('../initPlayer');
  });
  if (!loaded) throw new Error('initPlayer did not load');
  return loaded;
}

function hangNextSetupPlayer(): void {
  trackPlayerDouble.default.setupPlayer.mockImplementationOnce(() => new Promise(() => {}));
}

const STILL_PENDING = 'still pending';

// The defect under test is a promise that never settles, so record how it settled
// instead of awaiting it: an await would hang the run rather than fail an assertion.
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
    const { ensurePlayerSetup } = freshInitPlayer();

    await ensurePlayerSetup();
    await ensurePlayerSetup();

    expect(__player.calls('setupPlayer')).toHaveLength(1);
  });

  it('retries setup after a failed attempt instead of replaying the cached rejection', async () => {
    const { ensurePlayerSetup } = freshInitPlayer();
    __player.failNext('setupPlayer', new Error('native module not ready'));

    await expect(ensurePlayerSetup()).rejects.toThrow('native module not ready');
    await expect(ensurePlayerSetup()).resolves.toBeUndefined();

    expect(__player.calls('setupPlayer')).toHaveLength(2);
    expect(__player.calls('updateOptions')).toHaveLength(1);
  });

  it('shares one in-flight attempt between concurrent callers', async () => {
    const { ensurePlayerSetup } = freshInitPlayer();

    await Promise.all([ensurePlayerSetup(), ensurePlayerSetup()]);

    expect(__player.calls('setupPlayer')).toHaveLength(1);
  });

  describe('when native setup never settles', () => {
    beforeEach(() => jest.useFakeTimers());
    afterEach(() => jest.useRealTimers());

    it('rejects the caller once setup outlives its budget instead of waiting forever', async () => {
      const { ensurePlayerSetup, PLAYER_SETUP_TIMEOUT_MS, PlayerSetupTimeoutError } =
        freshInitPlayer();
      hangNextSetupPlayer();

      const settlement = settlementOf(ensurePlayerSetup());

      await jest.advanceTimersByTimeAsync(PLAYER_SETUP_TIMEOUT_MS - 1);
      expect(settlement()).toBe(STILL_PENDING);

      await jest.advanceTimersByTimeAsync(1);
      expect(settlement()).toBeInstanceOf(PlayerSetupTimeoutError);
    });

    it('starts a fresh attempt after a timed-out one instead of caching the hung call', async () => {
      const { ensurePlayerSetup, PLAYER_SETUP_TIMEOUT_MS } = freshInitPlayer();
      hangNextSetupPlayer();
      const timedOut = ensurePlayerSetup().catch((err: unknown) => err);
      await jest.advanceTimersByTimeAsync(PLAYER_SETUP_TIMEOUT_MS);
      await timedOut;

      await expect(ensurePlayerSetup()).resolves.toBeUndefined();

      expect(__player.calls('setupPlayer')).toHaveLength(2);
      expect(__player.calls('updateOptions')).toHaveLength(1);
    });

    it('clears the deadline once setup settles in time', async () => {
      const { ensurePlayerSetup } = freshInitPlayer();

      await ensurePlayerSetup();

      expect(jest.getTimerCount()).toBe(0);
    });
  });
});
