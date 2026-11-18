import * as Crypto from 'expo-crypto';

const { __crypto } = Crypto as unknown as {
  __crypto: { nextUuid(id: string): void; reset(): void };
};

const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

describe('expo-crypto double', () => {
  it('mints a fresh v4 uuid on each call', () => {
    const first = Crypto.randomUUID();

    expect(first).toMatch(UUID_V4);
    expect(Crypto.randomUUID()).not.toBe(first);
  });

  it('hands out queued ids in order, then mints again', () => {
    __crypto.nextUuid('first-id');
    __crypto.nextUuid('second-id');

    expect(Crypto.randomUUID()).toBe('first-id');
    expect(Crypto.randomUUID()).toBe('second-id');
    expect(Crypto.randomUUID()).toMatch(UUID_V4);
  });

  it('drops queued ids on reset', () => {
    __crypto.nextUuid('never-used');
    __crypto.reset();

    expect(Crypto.randomUUID()).toMatch(UUID_V4);
  });
});
