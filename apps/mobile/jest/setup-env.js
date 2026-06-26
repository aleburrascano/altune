// Jest setupFiles entry — runs BEFORE the test framework is installed and
// before any module is required. Sets EXPO_PUBLIC_SUPABASE_* defaults so
// modules that import the supabase singleton at load time don't throw
// "supabaseUrl is required" in the test environment. Tests that care about
// the Supabase client mock it directly; this only unblocks transitive
// imports.

process.env.EXPO_PUBLIC_SUPABASE_URL = process.env.EXPO_PUBLIC_SUPABASE_URL || 'https://fixture.supabase.co';
process.env.EXPO_PUBLIC_SUPABASE_ANON_KEY = process.env.EXPO_PUBLIC_SUPABASE_ANON_KEY || 'fixture-anon-key';

// expo-audio requires native modules that don't exist in Jest.
// Mock the API surface used by the playback feature.
jest.mock('expo-audio', () => {
  const mockPlayer = {
    play: jest.fn(),
    pause: jest.fn(),
    seekTo: jest.fn(),
    setActiveForLockScreen: jest.fn(),
    currentTime: 0,
    duration: 0,
    playing: false,
    paused: true,
    isBuffering: false,
    isLoaded: false,
  };
  return {
    useAudioPlayer: jest.fn(() => mockPlayer),
    useAudioPlayerStatus: jest.fn(() => ({
      playing: false,
      paused: true,
      isBuffering: false,
      isLoaded: false,
      currentTime: 0,
      duration: 0,
    })),
    setAudioModeAsync: jest.fn().mockResolvedValue(undefined),
  };
});

// expo-web-browser opens a native auth session; mock the surface useOAuth uses
// so auth screens that render the OAuth buttons load without native modules.
jest.mock('expo-web-browser', () => ({
  maybeCompleteAuthSession: jest.fn(),
  openAuthSessionAsync: jest.fn().mockResolvedValue({ type: 'cancel' }),
}));

// expo-blur's BlurView is a native view; render it as a plain View in tests
// (the auth hero background mounts it).
jest.mock('expo-blur', () => {
  const { View } = require('react-native');
  return { BlurView: View };
});

// expo-linear-gradient: render as a plain View in tests (the auth hero uses it
// for the artwork tiles + veil).
jest.mock('expo-linear-gradient', () => {
  const { View } = require('react-native');
  return { LinearGradient: View };
});

// react-native-svg: stub Svg + Path to plain Views so the Google logo renders
// in tests without the native module.
jest.mock('react-native-svg', () => {
  const { View } = require('react-native');
  return { __esModule: true, default: View, Svg: View, Path: View };
});
