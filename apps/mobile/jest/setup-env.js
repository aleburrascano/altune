// Jest setupFiles entry — runs BEFORE the test framework is installed and
// before any module is required. Sets EXPO_PUBLIC_SUPABASE_* defaults so
// modules that import the supabase singleton at load time don't throw
// "supabaseUrl is required" in the test environment. Tests that care about
// the Supabase client mock it directly; this only unblocks transitive
// imports.

process.env.EXPO_PUBLIC_SUPABASE_URL = process.env.EXPO_PUBLIC_SUPABASE_URL || 'https://fixture.supabase.co';
process.env.EXPO_PUBLIC_SUPABASE_ANON_KEY = process.env.EXPO_PUBLIC_SUPABASE_ANON_KEY || 'fixture-anon-key';

// expo-av requires native module ExponentAV which doesn't exist in Jest.
// Mock the Audio API surface used by the playback feature.
jest.mock('expo-av', () => ({
  Audio: {
    Sound: { createAsync: jest.fn().mockResolvedValue({ sound: { unloadAsync: jest.fn(), pauseAsync: jest.fn(), playAsync: jest.fn(), setPositionAsync: jest.fn() } }) },
    setAudioModeAsync: jest.fn().mockResolvedValue(undefined),
  },
}));
