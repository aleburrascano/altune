jest.mock(
  'react-native-safe-area-context',
  () => require('react-native-safe-area-context/jest/mock').default,
);

const { fakeFetch, __http } = require('./doubles/fetch.js');

global.fetch = fakeFetch;

beforeEach(() => {
  require('expo-file-system').__fs.reset();
  require('expo-secure-store').__secureStore.reset();
  require('react-native-track-player').__player.reset();
  __http.reset();
});
