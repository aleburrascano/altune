jest.mock(
  'react-native-safe-area-context',
  () => require('react-native-safe-area-context/jest/mock').default,
);

beforeEach(() => {
  require('expo-file-system').__fs.reset();
  require('expo-secure-store').__secureStore.reset();
  require('react-native-track-player').__player.reset();
});
