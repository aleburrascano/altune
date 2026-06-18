/**
 * supabaseClient — verifies the SDK is constructed with the expo-secure-store
 * storage adapter (AC: the spec's expo-secure-store Risk pinned by Slice 9a's
 * failing test).
 *
 * Notes:
 * - jest.mock factories cannot close over outer-scope variables unless they're
 *   prefixed with `mock` (jest's allowed convention).
 * - Jest in this project doesn't enable --experimental-vm-modules; use
 *   jest.isolateModules + require() to re-evaluate the singleton per test.
 */
/* eslint-disable @typescript-eslint/no-require-imports */
import * as SecureStore from 'expo-secure-store';

jest.mock('expo-secure-store', () => ({
  getItemAsync: jest.fn(async () => null),
  setItemAsync: jest.fn(async () => undefined),
  deleteItemAsync: jest.fn(async () => undefined),
  AFTER_FIRST_UNLOCK: 'afterFirstUnlock',
}));

const mockCreateClient = jest.fn((..._args: unknown[]) => ({ auth: {} }));
jest.mock('@supabase/supabase-js', () => ({
  createClient: (...args: unknown[]) => mockCreateClient(...args),
}));

describe('supabaseClient', () => {
  beforeEach(() => {
    mockCreateClient.mockClear();
  });

  it('constructs the SDK with the expo-secure-store storage adapter', () => {
    jest.isolateModules(() => {
      require('../api/supabaseClient');
    });

    expect(mockCreateClient).toHaveBeenCalledTimes(1);
    const args = mockCreateClient.mock.calls[0] as unknown[];
    const options = args[2] as { auth?: { storage?: Record<string, unknown> } };
    expect(options?.auth?.storage).toBeDefined();

    const adapter = options!.auth!.storage as {
      getItem: (k: string) => Promise<string | null>;
      setItem: (k: string, v: string) => Promise<void>;
      removeItem: (k: string) => Promise<void>;
    };
    void adapter.getItem('k');
    void adapter.setItem('k', 'v');
    void adapter.removeItem('k');

    const opts = { keychainAccessible: SecureStore.AFTER_FIRST_UNLOCK };
    expect(SecureStore.getItemAsync).toHaveBeenCalledWith('k', opts);
    expect(SecureStore.setItemAsync).toHaveBeenCalledWith('k', 'v', opts);
    expect(SecureStore.deleteItemAsync).toHaveBeenCalledWith('k', opts);
  });

  it('configures persistSession and autoRefreshToken', () => {
    jest.isolateModules(() => {
      require('../api/supabaseClient');
    });

    const args = mockCreateClient.mock.calls[0] as unknown[];
    const options = args[2] as { auth?: { persistSession?: boolean; autoRefreshToken?: boolean } };
    expect(options?.auth?.persistSession).toBe(true);
    expect(options?.auth?.autoRefreshToken).toBe(true);
  });
});
