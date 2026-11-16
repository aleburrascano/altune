import { isWebPlatform } from '@shared/device/core/isWebPlatform.web';

describe('isWebPlatform (web)', () => {
  it('returns true', () => {
    expect(isWebPlatform()).toBe(true);
  });
});
