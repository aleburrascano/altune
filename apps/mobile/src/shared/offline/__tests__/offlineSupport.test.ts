function offlineDownloadsSupportedOn(os: string): boolean {
  let loaded: boolean | undefined;
  jest.isolateModules(() => {
    require('react-native').Platform.OS = os;
    loaded = require('../offlineSupport').offlineDownloadsSupported;
  });
  return loaded as boolean;
}

describe('offlineDownloadsSupported', () => {
  it('is false on web, where there is no filesystem to pin audio to', () => {
    expect(offlineDownloadsSupportedOn('web')).toBe(false);
  });

  it.each(['ios', 'android'])('is true on %s, where downloads are pinned to disk', (os) => {
    expect(offlineDownloadsSupportedOn(os)).toBe(true);
  });
});
