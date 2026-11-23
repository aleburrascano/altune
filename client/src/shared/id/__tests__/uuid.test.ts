import { randomUuid } from '@shared/id';

const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

describe('randomUuid', () => {
  it('returns a v4 uuid', () => {
    expect(randomUuid()).toMatch(UUID_V4);
  });

  it('returns a different value on each call', () => {
    expect(randomUuid()).not.toBe(randomUuid());
  });
});
