const enumProxy = new Proxy({}, { get: (_target, prop) => String(prop) });

const methods = new Map();
const pendingFailures = new Map();

let playbackState = { state: undefined };
let progress = { position: 0, duration: 0, buffered: 0 };
const eventListeners = new Map();

function methodNamed(name) {
  if (!methods.has(name)) {
    methods.set(
      name,
      jest.fn(async () => {
        const failure = pendingFailures.get(name);
        if (failure === undefined) return undefined;
        pendingFailures.delete(name);
        throw failure;
      }),
    );
  }
  return methods.get(name);
}

const player = new Proxy(
  {},
  {
    get: (_target, prop) => (typeof prop === 'string' ? methodNamed(prop) : undefined),
    has: () => true,
  },
);

const __player = {
  reset() {
    for (const mock of methods.values()) mock.mockClear();
    pendingFailures.clear();
    eventListeners.clear();
    playbackState = { state: undefined };
    progress = { position: 0, duration: 0, buffered: 0 };
  },

  failNext(method, error) {
    pendingFailures.set(method, error ?? new Error(`injected ${method} failure`));
  },

  setState(state) {
    playbackState = { state };
  },

  setProgress(next) {
    progress = { ...progress, ...next };
  },

  emit(event, payload) {
    for (const listener of eventListeners.get(event) ?? []) listener(payload);
  },

  calls(method) {
    return methodNamed(method).mock.calls;
  },
};

module.exports = {
  __esModule: true,
  default: player,
  State: enumProxy,
  Capability: enumProxy,
  Event: enumProxy,
  RepeatMode: enumProxy,
  AppKilledPlaybackBehavior: enumProxy,
  usePlaybackState: () => playbackState,
  useProgress: () => progress,
  useTrackPlayerEvents: (events, listener) => {
    for (const event of events) {
      const existing = eventListeners.get(event) ?? [];
      if (!existing.includes(listener)) eventListeners.set(event, [...existing, listener]);
    }
  },
  __player,
};
