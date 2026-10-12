function offlineDownloadsSupportedOn(os: string): boolean {
  let loaded: boolean | undefined;
  jest.isolateModules(() => {
    require('react-native').Platform.OS = os;
    loaded = require('@shared/offline/offlineSupport').offlineDownloadsSupported;
  });
  return loaded as boolean;
}

describe('offlineDownloadsSupported', () => {
  it.each(['ios', 'android'])('is true on %s, where downloads are pinned to disk', (os) => {
    expect(offlineDownloadsSupportedOn(os)).toBe(true);
  });
});
