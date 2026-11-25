import { isWebPlatform } from '@shared/device';

describe('isWebPlatform (web)', () => {
  it('returns true', () => {
    expect(isWebPlatform()).toBe(true);
  });
});
