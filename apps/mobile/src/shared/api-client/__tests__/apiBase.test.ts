import type * as ApiClient from '../index';

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

type DevGlobal = { __DEV__: boolean };

const ORIGINAL_URL = process.env.EXPO_PUBLIC_API_URL;
const ORIGINAL_DEV = (globalThis as unknown as DevGlobal).__DEV__;

function loadApiBase(value: string | undefined, dev: boolean): string {
  if (value === undefined) delete process.env.EXPO_PUBLIC_API_URL;
  else process.env.EXPO_PUBLIC_API_URL = value;
  (globalThis as unknown as DevGlobal).__DEV__ = dev;
  let apiBase = '';
  jest.isolateModules(() => {
    apiBase = (require('../index') as typeof ApiClient).apiBase;
  });
  return apiBase;
}

afterEach(() => {
  if (ORIGINAL_URL === undefined) delete process.env.EXPO_PUBLIC_API_URL;
  else process.env.EXPO_PUBLIC_API_URL = ORIGINAL_URL;
  (globalThis as unknown as DevGlobal).__DEV__ = ORIGINAL_DEV;
});

describe('apiBase startup validation', () => {
  it('keeps the loopback default for a development build with no EXPO_PUBLIC_API_URL', () => {
    expect(loadApiBase(undefined, true)).toBe('http://127.0.0.1:8000');
  });

  it('uses a well-formed EXPO_PUBLIC_API_URL in a production build', () => {
    expect(loadApiBase('https://altune.example.org', false)).toBe('https://altune.example.org');
  });

  it('accepts a host with a port and a path prefix', () => {
    expect(loadApiBase('http://192.168.1.20:8000/api', true)).toBe('http://192.168.1.20:8000/api');
  });

  it.each([[undefined], ['']])(
    'fails at module load, naming the variable, when a production build has %p',
    (value) => {
      expect(() => loadApiBase(value, false)).toThrow(
        'Missing required environment variable EXPO_PUBLIC_API_URL',
      );
    },
  );

  it('treats an empty value in a development build as unset rather than as a relative base', () => {
    expect(loadApiBase('', true)).toBe('http://127.0.0.1:8000');
  });

  it.each([
    ['http://altune.example.org'],
    ['http://altune.example.org:8000/api'],
    ['http://192.168.1.20:8000'],
    ['http://127.0.0.1.altune.example.org'],
    ['http://localhost.altune.example.org'],
    ['http://127.0.0.1@altune.example.org'],
  ])('fails at module load for the plaintext remote base %p in a production build', (value) => {
    expect(() => loadApiBase(value, false)).toThrow(
      `Insecure EXPO_PUBLIC_API_URL "${value}": a release build requires https:// for a remote host`,
    );
  });

  it.each([['http://127.0.0.1:8000'], ['http://localhost:8000'], ['http://[::1]:8000']])(
    'keeps accepting the on-device base %p in a production build',
    (value) => {
      expect(loadApiBase(value, false)).toBe(value);
    },
  );

  it('keeps accepting a plaintext LAN base in a development build', () => {
    expect(loadApiBase('http://192.168.1.20:8000', true)).toBe('http://192.168.1.20:8000');
  });

  it.each([
    ['altune.example.org'],
    ['ftp://altune.example.org'],
    ['https://'],
    ['https://altune.example.org/'],
    [' https://altune.example.org'],
    ['https://altune.example.org?x=1'],
  ])('fails at module load, naming the variable, for the malformed value %p', (value) => {
    expect(() => loadApiBase(value, true)).toThrow(/EXPO_PUBLIC_API_URL/);
    expect(() => loadApiBase(value, false)).toThrow(/EXPO_PUBLIC_API_URL/);
  });
});
