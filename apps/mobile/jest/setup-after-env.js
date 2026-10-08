const { configure } = require('@testing-library/react-native');

configure({ asyncUtilTimeout: 5000 });

jest.mock(
  'react-native-safe-area-context',
  () => require('react-native-safe-area-context/jest/mock').default,
);

jest.mock('react-native-reanimated', () => ({
  __esModule: true,
  default: { View: jest.requireActual('react-native').View },
  useAnimatedStyle: () => ({}),
}));
jest.mock('react-native-gesture-handler/ReanimatedSwipeable', () => ({
  __esModule: true,
  default: ({ children }) => children,
}));

const { fakeFetch, __http } = require('./doubles/fetch.js');

global.fetch = fakeFetch;

function resetOutbox() {
  const outbox = require('../src/shared/telemetry/outbox');
  if (typeof outbox._resetOutboxForTest === 'function') outbox._resetOutboxForTest();
}

beforeEach(() => {
  require('expo-file-system').__fs.reset();
  require('expo-secure-store').__secureStore.reset();
  require('react-native-track-player').__player.reset();
  __http.reset();
  resetOutbox();
});
