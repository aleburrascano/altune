import { apiBase, installApiBase } from '@shared/apiConfig';

const NAMES_THE_VARIABLE = /EXPO_PUBLIC_API_URL/;

function apiBaseBeforeAnyInstall(): () => string {
  let fresh: () => string = apiBase;
  jest.isolateModules(() => {
    fresh = require('@shared/apiConfig').apiBase;
  });
  return fresh;
}

function installedApiBase(value: string | undefined, dev: boolean): string {
  installApiBase(value, dev);
  return apiBase();
}

describe('apiBase startup validation', () => {
  it('keeps the loopback default for a development build with no EXPO_PUBLIC_API_URL', () => {
    expect(installedApiBase(undefined, true)).toBe('http://127.0.0.1:8000');
  });

  it('uses a well-formed EXPO_PUBLIC_API_URL in a production build', () => {
    expect(installedApiBase('https://altune.example.org', false)).toBe(
      'https://altune.example.org',
    );
  });

  it('accepts a host with a port and a path prefix', () => {
    expect(installedApiBase('http://192.168.1.20:8000/api', true)).toBe(
      'http://192.168.1.20:8000/api',
    );
  });

  it.each([[undefined], ['']])(
    'fails at startup, naming the variable, when a production build has %p',
    (value) => {
      expect(() => installedApiBase(value, false)).toThrow(
        'Missing required environment variable EXPO_PUBLIC_API_URL',
      );
    },
  );

  it('treats an empty value in a development build as unset rather than as a relative base', () => {
    expect(installedApiBase('', true)).toBe('http://127.0.0.1:8000');
  });

  it.each([
    ['http://altune.example.org'],
    ['http://altune.example.org:8000/api'],
    ['http://192.168.1.20:8000'],
    ['http://127.0.0.1.altune.example.org'],
    ['http://localhost.altune.example.org'],
    ['http://127.0.0.1@altune.example.org'],
    ['http://altune.example.org/localhost'],
  ])('fails at startup for the plaintext remote base %p in a production build', (value) => {
    expect(() => installedApiBase(value, false)).toThrow(
      `Insecure EXPO_PUBLIC_API_URL "${value}": a release build requires https:// for a remote host`,
    );
  });

  it.each([
    ['http://127.0.0.1:8000'],
    ['http://localhost:8000'],
    ['http://[::1]:8000'],
    ['http://localhost'],
  ])('keeps accepting the on-device base %p in a production build', (value) => {
    expect(installedApiBase(value, false)).toBe(value);
  });

  it('keeps accepting a plaintext LAN base in a development build', () => {
    expect(installedApiBase('http://192.168.1.20:8000', true)).toBe('http://192.168.1.20:8000');
  });

  it.each([
    ['altune.example.org'],
    ['ftp://altune.example.org'],
    ['https://'],
    ['https://altune.example.org/'],
    [' https://altune.example.org'],
    ['https://altune.example.org?x=1'],
  ])('fails at startup, naming the variable, for the malformed value %p', (value) => {
    expect(() => installedApiBase(value, true)).toThrow(NAMES_THE_VARIABLE);
    expect(() => installedApiBase(value, false)).toThrow(NAMES_THE_VARIABLE);
  });
});

describe('apiBase before the app installs one', () => {
  it('is the loopback development default', () => {
    expect(apiBaseBeforeAnyInstall()()).toBe('http://127.0.0.1:8000');
  });
});
