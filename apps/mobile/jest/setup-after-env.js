// Runs AFTER the jest framework is installed (so `jest` is available).
//
// The Screen primitive calls useSafeAreaInsets(), which throws when no
// SafeAreaProvider is mounted — but the auth screens render bare in their
// tests. Use the library's official jest mock (returns zero insets). ADR-0008.
jest.mock(
  'react-native-safe-area-context',
  () =>
    // The mock module uses `export default {...}`, so its named members
    // (useSafeAreaInsets, SafeAreaProvider, ...) live on `.default`.
    require('react-native-safe-area-context/jest/mock').default,
);
