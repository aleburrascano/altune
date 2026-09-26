import type { ReactElement } from 'react';

type SessionStorage = { setItem: (key: string, value: string) => Promise<void> };

let capturedStorage: SessionStorage | undefined;

jest.mock('@supabase/supabase-js', () => ({
  createClient: (_url: string, _key: string, options: { auth: { storage: SessionStorage } }) => {
    capturedStorage = options.auth.storage;
    return {};
  },
}));

jest.mock('@features/playback/hooks/trackPlayerProvider', () => ({
  TrackPlayerPlaybackProvider: function TrackPlayerPlaybackProvider() {
    return null;
  },
}));

function onPlatform<T>(os: string, load: () => T): T {
  let loaded: T | undefined;
  jest.isolateModules(() => {
    require('react-native').Platform.OS = os;
    loaded = load();
  });
  return loaded as T;
}

function selectedProviderName(os: string): string {
  return onPlatform(os, () => {
    const { PlaybackProvider } = require('@features/playback/hooks/PlaybackProvider');
    const element: ReactElement<unknown, { name: string }> = PlaybackProvider({ children: null });
    return element.type.name;
  });
}

describe('mh10: the playback provider and session store each platform selects', () => {
  it.each(['ios', 'android'])('selects the track-player provider on %s', (os) => {
    expect(selectedProviderName(os)).toBe('TrackPlayerPlaybackProvider');
  });

  it('selects the web audio provider on web', () => {
    expect(selectedProviderName('web')).toBe('WebPlaybackProvider');
  });

  it.each(['ios', 'android'])('persists the native session in secure-store on %s', async (os) => {
    const secureStore = onPlatform(os, () => {
      capturedStorage = undefined;
      require('@shared/auth/supabaseClient');
      return require('expo-secure-store').__secureStore;
    });

    await capturedStorage?.setItem('sb-session', 'serialized-session');

    expect(secureStore.read('sb-session')).toBe('serialized-session');
  });

  it.each(['ios', 'android'])('keeps the no-op provider in Expo Go on %s', (os) => {
    const selected = onPlatform(os, () => {
      jest.doMock('@shared/playback/isExpoGo', () => ({ isExpoGo: true }));
      const { PlaybackProvider } = require('@features/playback/hooks/PlaybackProvider');
      const element: ReactElement<unknown, { name: string }> = PlaybackProvider({ children: null });
      return element.type.name;
    });

    expect(selected).toBe('ExpoGoPlaybackProvider');
  });
});
