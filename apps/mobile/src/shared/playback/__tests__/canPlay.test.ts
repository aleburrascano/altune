import type { AcquisitionStatus } from '@shared/api-client/types';

import { canPlay } from '../canPlay';

describe('canPlay', () => {
  it.each([
    ['pending', false],
    ['ready', true],
    ['failed', false],
  ] as [AcquisitionStatus, boolean][])('acquisition status %s -> %s', (status, expected) => {
    expect(canPlay(status)).toBe(expected);
  });

  it('gates a Track with no acquisition status recorded yet', () => {
    expect(canPlay(null)).toBe(false);
  });

  it('gates a Track whose acquisition status was never fetched', () => {
    expect(canPlay(undefined)).toBe(false);
  });
});
