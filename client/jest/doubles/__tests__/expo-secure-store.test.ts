import * as SecureStore from 'expo-secure-store';

const { __secureStore } = SecureStore as unknown as {
  __secureStore: { read(key: string): string | undefined; failNext(op: string, e?: Error): void };
};

describe('secure-store double', () => {
  it('round-trips a token', async () => {
    await SecureStore.setItemAsync('session', 'token-abc');
    await expect(SecureStore.getItemAsync('session')).resolves.toBe('token-abc');
  });

  it('returns null for an absent key rather than throwing', async () => {
    await expect(SecureStore.getItemAsync('missing')).resolves.toBeNull();
  });

  it('injects a keychain failure', async () => {
    __secureStore.failNext('set', new Error('keychain unavailable'));
    await expect(SecureStore.setItemAsync('session', 'token')).rejects.toThrow(
      'keychain unavailable',
    );
    expect(__secureStore.read('session')).toBeUndefined();
  });
});
