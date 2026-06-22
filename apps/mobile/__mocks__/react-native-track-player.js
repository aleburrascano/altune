/**
 * Jest manual mock for react-native-track-player.
 *
 * The real package binds a native module at import time, which throws under
 * jest (no native runtime) — the same reason it can't load in Expo Go. This
 * stub lets any test that transitively imports the track-player-backed playback
 * provider run. Auto-applied by jest for this node_modules module.
 */
const enumProxy = new Proxy({}, { get: (_target, prop) => String(prop) });

module.exports = {
  __esModule: true,
  default: new Proxy({}, { get: () => jest.fn() }),
  State: enumProxy,
  Capability: enumProxy,
  Event: enumProxy,
  RepeatMode: enumProxy,
  AppKilledPlaybackBehavior: enumProxy,
  usePlaybackState: () => ({ state: undefined }),
  useProgress: () => ({ position: 0, duration: 0, buffered: 0 }),
  useTrackPlayerEvents: () => {},
};
