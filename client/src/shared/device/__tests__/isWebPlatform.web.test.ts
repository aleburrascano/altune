import { isWebPlatform } from '@shared/device/isWebPlatform.web';

describe('isWebPlatform (web)', () => {
  it('returns true', () => {
    expect(isWebPlatform()).toBe(true);
  });
});
