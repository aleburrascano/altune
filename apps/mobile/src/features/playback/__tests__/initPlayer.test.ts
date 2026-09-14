const trackPlayerDouble = jest.requireMock('react-native-track-player');
const { __player } = trackPlayerDouble;

// initPlayer caches its setup attempt in module state, so each test loads its own copy.
function freshEnsurePlayerSetup(): () => Promise<void> {
  let ensure: (() => Promise<void>) | undefined;
  jest.isolateModules(() => {
    jest.doMock('react-native-track-player', () => trackPlayerDouble);
    ensure = require('../initPlayer').ensurePlayerSetup;
  });
  if (!ensure) throw new Error('initPlayer did not load');
  return ensure;
}

describe('ensurePlayerSetup', () => {
  it('sets the player up only once across repeated successful calls', async () => {
    const ensurePlayerSetup = freshEnsurePlayerSetup();

    await ensurePlayerSetup();
    await ensurePlayerSetup();

    expect(__player.calls('setupPlayer')).toHaveLength(1);
  });

  it('retries setup after a failed attempt instead of replaying the cached rejection', async () => {
    const ensurePlayerSetup = freshEnsurePlayerSetup();
    __player.failNext('setupPlayer', new Error('native module not ready'));

    await expect(ensurePlayerSetup()).rejects.toThrow('native module not ready');
    await expect(ensurePlayerSetup()).resolves.toBeUndefined();

    expect(__player.calls('setupPlayer')).toHaveLength(2);
    expect(__player.calls('updateOptions')).toHaveLength(1);
  });

  it('shares one in-flight attempt between concurrent callers', async () => {
    const ensurePlayerSetup = freshEnsurePlayerSetup();

    await Promise.all([ensurePlayerSetup(), ensurePlayerSetup()]);

    expect(__player.calls('setupPlayer')).toHaveLength(1);
  });
});
