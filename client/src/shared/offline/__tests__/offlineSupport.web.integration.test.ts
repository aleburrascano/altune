import { offlineDownloadsSupported } from '@shared/offline/offlineSupport';

describe('offlineDownloadsSupported on web', () => {
  it('is false, where there is no filesystem to pin audio to', () => {
    expect(offlineDownloadsSupported).toBe(false);
  });
});
