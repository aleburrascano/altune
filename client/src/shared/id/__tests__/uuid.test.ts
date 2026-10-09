import * as Crypto from 'expo-crypto';

import { randomUuid } from '@shared/id/uuid';

afterEach(() => {
  jest.restoreAllMocks();
});

describe('randomUuid', () => {
  it('returns a v4 uuid', () => {
    expect(randomUuid()).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i,
    );
  });

  it('returns a different value on each call', () => {
    expect(randomUuid()).not.toBe(randomUuid());
  });

  it('delegates to Crypto.randomUUID', () => {
    const spy = jest.spyOn(Crypto, 'randomUUID').mockReturnValue('fixed-id');
    expect(randomUuid()).toBe('fixed-id');
    expect(spy).toHaveBeenCalledTimes(1);
  });
});
