const { configure } = require('@testing-library/react-native');

configure({ asyncUtilTimeout: 5000 });

const { fakeFetch, __http } = require('./doubles/fetch.js');

global.fetch = fakeFetch;

function installFreshTelemetry() {
  const telemetry = require('../src/shared/telemetry');
  const { createMemoryFileStore } = require('./fakes/files/inMemoryFileStore');
  const { warn } = require('../src/shared/log');
  const store = telemetry.createOutboxStore(createMemoryFileStore(), { warn });
  telemetry.installTelemetry(telemetry.createAppTelemetry({ store }));
}

function resetLogger() {
  require('../src/shared/log').setLogger();
}

function resetAppStateSource() {
  require('../src/shared/lifecycle').setAppStateSource();
}

const { ensureAppAuthClient } = require('./appAuthClient.js');

beforeAll(ensureAppAuthClient);

afterAll(installFreshTelemetry);

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
