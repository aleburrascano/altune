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
