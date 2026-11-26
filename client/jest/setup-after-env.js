function restoreRealTimersBeforeRntlCleanup() {
  jest.useRealTimers();
}

if (typeof document !== 'undefined') {
  afterEach(restoreRealTimersBeforeRntlCleanup);
}

const { configure } = require('@testing-library/react-native');

configure({ asyncUtilTimeout: 5000 });

const { fakeFetch, __http } = require('./doubles/fetch.js');

global.fetch = fakeFetch;

if (typeof window !== 'undefined' && typeof window.matchMedia !== 'function') {
  window.matchMedia = (query) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  });
}

function freshTelemetry() {
  const telemetry = require('../src/shared/telemetry');
  const { createMemoryFileStore } = require('./fakes/files/inMemoryFileStore');
  const { warn } = require('../src/shared/log');
  const store = telemetry.createOutboxStore(createMemoryFileStore(), { warn });
  return telemetry.createAppTelemetry({ store });
}

function installFreshTelemetry() {
  require('../src/shared/telemetry').installTelemetry(freshTelemetry());
}

function installStoppedTelemetry() {
  const StoppedTelemetry = freshTelemetry();
  StoppedTelemetry.dispose();
  require('../src/shared/telemetry').installTelemetry(StoppedTelemetry);
}

function resetLogger() {
  require('../src/shared/log').setLogger();
}

function resetAppStateSource() {
  require('../src/shared/lifecycle').setAppStateSource();
}

const { ensureAppAuthClient } = require('./appAuthClient.js');

beforeAll(ensureAppAuthClient);

async function stopTelemetryWork() {
  __http.abortHanging();
  await require('../src/shared/telemetry').stopTelemetry();
  installStoppedTelemetry();
}

function stopOfflineWork() {
  require('../src/shared/offline').stopOffline();
}

afterEach(stopTelemetryWork);

afterAll(stopTelemetryWork);

afterAll(stopOfflineWork);

beforeEach(() => {
  ensureAppAuthClient();
  resetLogger();
  resetAppStateSource();
  require('expo-file-system').__fs.reset();
  require('expo-secure-store').__secureStore.reset();
  require('expo-crypto').__crypto.reset();
  require('react-native-track-player').__player.reset();
  __http.reset();
  installFreshTelemetry();
});
