import Constants, { ExecutionEnvironment } from 'expo-constants';

/**
 * True when running inside the stock **Expo Go** app, where custom native
 * modules are not bundled. `react-native-track-player` is such a module, so its
 * mere import crashes at startup in Expo Go. Playback code uses this flag to
 * swap to a no-op audio backend, letting the app boot in Expo Go with everything
 * except audio playback working. A development build (expo-dev-client) reports a
 * non-storeClient environment and gets the real track-player backend.
 */
export const isExpoGo = Constants.executionEnvironment === ExecutionEnvironment.StoreClient;
