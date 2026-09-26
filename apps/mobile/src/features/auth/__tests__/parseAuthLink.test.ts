import { Platform } from 'react-native';

import appJson from '../../../../app.json';
import type * as ParseAuthLinkModule from '../parseAuthLink';
import { CONFIRM_REDIRECT_URL, OAUTH_REDIRECT_URL, parseAuthLink } from '../parseAuthLink';
import { authRedirectUrl } from '../parseAuthLink';

function loadWithExpoScheme(scheme: unknown): typeof ParseAuthLinkModule {
  jest.resetModules();
  jest.doMock('expo-constants', () => ({
    __esModule: true,
    default: { expoConfig: scheme === undefined ? {} : { scheme } },
  }));
  return require('../parseAuthLink') as typeof ParseAuthLinkModule;
}

afterEach(() => {
  jest.dontMock('expo-constants');
  jest.resetModules();
});

describe('parseAuthLink — the scheme comes from the expo config', () => {
  it("accepts app.json's configured scheme and builds its redirects from it", () => {
    const configured = appJson.expo.scheme;

    expect(parseAuthLink(`${configured}://auth/recovery?type=recovery`)).toEqual({
      kind: 'recovery',
      params: { type: 'recovery' },
    });
    expect(OAUTH_REDIRECT_URL).toBe(`${configured}://auth/callback`);
    expect(CONFIRM_REDIRECT_URL).toBe(`${configured}://auth/confirm`);
  });

  it('follows a rebranded scheme instead of the previous one', () => {
    const rebranded = loadWithExpoScheme('whitelabel');

    expect(rebranded.parseAuthLink('whitelabel://auth/recovery?type=recovery')).toEqual({
      kind: 'recovery',
      params: { type: 'recovery' },
    });
    expect(rebranded.parseAuthLink('altune://auth/recovery?type=recovery')).toEqual({
      kind: 'ignored',
    });
    expect(rebranded.OAUTH_REDIRECT_URL).toBe('whitelabel://auth/callback');
  });

  it('uses the first scheme when the config declares a list', () => {
    const multiScheme = loadWithExpoScheme(['whitelabel', 'legacy']);

    expect(multiScheme.OAUTH_REDIRECT_URL).toBe('whitelabel://auth/callback');
    expect(multiScheme.parseAuthLink('whitelabel://auth/callback?code=xyz')).toEqual({
      kind: 'oauth',
      params: { code: 'xyz' },
    });
  });

  it('matches a config scheme that is not lower-case', () => {
    const upperCased = loadWithExpoScheme('WhiteLabel');

    expect(upperCased.parseAuthLink('whitelabel://auth/callback?code=xyz')).toEqual({
      kind: 'oauth',
      params: { code: 'xyz' },
    });
  });

  it.each([
    ['absent', undefined],
    ['an empty string', ''],
    ['an empty list', []],
  ])('throws at import naming `scheme` when the config declares %s', (_case, scheme) => {
    expect(() => loadWithExpoScheme(scheme)).toThrow('scheme');
  });
});

describe('parseAuthLink', () => {
  it('parses a known recovery path', () => {
    const intent = parseAuthLink('altune://auth/recovery?token_hash=abc&type=recovery');

    expect(intent).toEqual({
      kind: 'recovery',
      params: { token_hash: 'abc', type: 'recovery' },
    });
  });

  it('rejects a prototype-property path like __proto__', () => {
    expect(parseAuthLink('altune://__proto__?code=x')).toEqual({ kind: 'ignored' });
    expect(parseAuthLink('altune://constructor?code=x')).toEqual({ kind: 'ignored' });
    expect(parseAuthLink('altune://hasOwnProperty?code=x')).toEqual({ kind: 'ignored' });
  });

  it('rejects a link whose param pairs exceed the cap', () => {
    const pairs = Array.from({ length: 500 }, (_, i) => `k${i}=v${i}`).join('&');

    expect(parseAuthLink(`altune://auth/callback?${pairs}`)).toEqual({ kind: 'ignored' });
  });

  it('rejects a link longer than the max url length', () => {
    const huge = 'a'.repeat(5000);

    expect(parseAuthLink(`altune://auth/callback?code=${huge}`)).toEqual({ kind: 'ignored' });
  });

  it('parses a differently-cased scheme (schemes are case-insensitive)', () => {
    expect(parseAuthLink('ALTUNE://auth/callback?code=xyz')).toEqual({
      kind: 'oauth',
      params: { code: 'xyz' },
    });
    expect(parseAuthLink('Altune://auth/callback?code=xyz')).toEqual({
      kind: 'oauth',
      params: { code: 'xyz' },
    });
  });

  it('ignores an unrelated scheme', () => {
    expect(parseAuthLink('https://auth/callback?code=xyz')).toEqual({ kind: 'ignored' });
  });

  it('rejects a param whose value carries a malformed percent-escape', () => {
    expect(parseAuthLink('altune://auth/callback?code=%zz')).toEqual({ kind: 'ignored' });
    expect(parseAuthLink('altune://auth/recovery?token_hash=abc%')).toEqual({ kind: 'ignored' });
    expect(parseAuthLink('altune://auth/confirm?token_hash=ab%4')).toEqual({ kind: 'ignored' });
  });

  it('rejects a param whose key carries a malformed percent-escape', () => {
    expect(parseAuthLink('altune://auth/callback?%zz=xyz')).toEqual({ kind: 'ignored' });
  });

  it('rejects a malformed percent-escape in the fragment as well as the query', () => {
    expect(parseAuthLink('altune://auth/callback#access_token=%zz')).toEqual({ kind: 'ignored' });
    expect(parseAuthLink('altune://auth/callback?code=ok#refresh_token=%')).toEqual({
      kind: 'ignored',
    });
  });

  it('rejects an escape sequence that decodes to ill-formed utf-8', () => {
    expect(parseAuthLink('altune://auth/callback?code=%C3%28')).toEqual({ kind: 'ignored' });
    expect(parseAuthLink('altune://auth/callback?code=%ED%A0%80')).toEqual({ kind: 'ignored' });
  });

  it('rejects a link once any one of its many params is malformed', () => {
    const pairs = Array.from({ length: 32 }, (_, i) => `k${i}=v${i}`).join('&');

    expect(parseAuthLink(`altune://auth/callback?${pairs}&code=%zz`)).toEqual({ kind: 'ignored' });
  });

  it('decodes valid percent-escapes in both keys and values', () => {
    expect(parseAuthLink('altune://auth/callback?%63ode=a%20b%F0%9F%98%80')).toEqual({
      kind: 'oauth',
      params: { code: 'a b\u{1F600}' },
    });
  });
});

describe('authRedirectUrl and parseAuthLink on web (#2837)', () => {
  afterEach(() => {
    Platform.OS = 'ios';
    Reflect.deleteProperty(globalThis, 'window');
  });

  it('builds every redirect from this origin instead of the altune scheme', () => {
    Platform.OS = 'web';
    Object.assign(globalThis, { window: { location: { origin: 'https://app.altune.example' } } });

    expect(authRedirectUrl('callback')).toBe('https://app.altune.example/auth/callback');
    expect(authRedirectUrl('confirm')).toBe('https://app.altune.example/auth/confirm');
    expect(authRedirectUrl('recovery')).toBe('https://app.altune.example/auth/recovery');
  });

  it('keeps the altune scheme on native even with a global window present', () => {
    Object.assign(globalThis, { window: { location: { origin: 'https://app.altune.example' } } });

    expect(authRedirectUrl('recovery')).toBe('altune://auth/recovery');
  });

  it('accepts this same origin for a known auth path', () => {
    Platform.OS = 'web';
    Object.assign(globalThis, { window: { location: { origin: 'https://app.altune.example' } } });

    expect(
      parseAuthLink('https://app.altune.example/auth/recovery?token_hash=abc&type=recovery'),
    ).toEqual({ kind: 'recovery', params: { token_hash: 'abc', type: 'recovery' } });
  });

  it('still ignores a foreign origin on web', () => {
    Platform.OS = 'web';
    Object.assign(globalThis, { window: { location: { origin: 'https://app.altune.example' } } });

    expect(parseAuthLink('https://evil.example/auth/recovery?type=recovery')).toEqual({
      kind: 'ignored',
    });
  });

  it('still ignores an unknown path at this same origin', () => {
    Platform.OS = 'web';
    Object.assign(globalThis, { window: { location: { origin: 'https://app.altune.example' } } });

    expect(parseAuthLink('https://app.altune.example/somewhere-else?code=x')).toEqual({
      kind: 'ignored',
    });
  });

  it('falls back to the altune scheme on web with no window to ask for an origin', () => {
    Platform.OS = 'web';

    expect(authRedirectUrl('confirm')).toBe('altune://auth/confirm');
  });

  it('ignores a foreign string merely because it is at least as long as this origin', () => {
    Platform.OS = 'web';
    Object.assign(globalThis, { window: { location: { origin: 'https://x.test' } } });
    const originPrefixLength = 'https://x.test/'.length;
    const foreign = 'a'.repeat(originPrefixLength) + 'auth/recovery?type=recovery';

    expect(parseAuthLink(foreign)).toEqual({ kind: 'ignored' });
  });

  it('ignores a bare path carrying neither the altune scheme nor a web origin', () => {
    expect(parseAuthLink('auth/recovery?type=recovery')).toEqual({ kind: 'ignored' });
  });

  it('ignores the same-origin https form on native', () => {
    Object.assign(globalThis, { window: { location: { origin: 'https://app.altune.example' } } });

    expect(parseAuthLink('https://app.altune.example/auth/recovery?type=recovery')).toEqual({
      kind: 'ignored',
    });
  });
});

describe('authRedirectUrl on web when window has no location to ask (#2837)', () => {
  afterEach(() => {
    Platform.OS = 'ios';
    Reflect.deleteProperty(globalThis, 'window');
  });

  it('falls back to the altune scheme', () => {
    Platform.OS = 'web';
    Object.assign(globalThis, { window: {} });

    expect(authRedirectUrl('callback')).toBe('altune://auth/callback');
  });
});

describe('parseAuthLink on web: only this exact origin and the three auth paths (#2837)', () => {
  const ORIGIN = 'https://app.altune.example';

  beforeEach(() => {
    Platform.OS = 'web';
    Object.assign(globalThis, { window: { location: { origin: ORIGIN } } });
  });

  afterEach(() => {
    Platform.OS = 'ios';
    Reflect.deleteProperty(globalThis, 'window');
  });

  it('accepts a same-origin oauth callback and a same-origin confirmation', () => {
    expect(parseAuthLink(`${ORIGIN}/auth/callback?code=xyz`)).toEqual({
      kind: 'oauth',
      params: { code: 'xyz' },
    });
    expect(parseAuthLink(`${ORIGIN}/auth/confirm?token_hash=abc&type=signup`)).toEqual({
      kind: 'confirm',
      params: { token_hash: 'abc', type: 'signup' },
    });
  });

  it.each([
    [
      'a lookalike host that extends this origin',
      'https://app.altune.example.evil.test/auth/callback?code=x',
    ],
    ['a host that ends with this host', 'https://evilapp.altune.example/auth/callback?code=x'],
    [
      'this origin as userinfo in front of a foreign host',
      'https://app.altune.example@evil.test/auth/callback?code=x',
    ],
    [
      'a foreign host with this origin smuggled after a backslash',
      'https://evil.test\\@app.altune.example/auth/callback?code=x',
    ],
    ['the same host downgraded to http', 'http://app.altune.example/auth/callback?code=x'],
    ['the same host on another port', 'https://app.altune.example:8443/auth/callback?code=x'],
    ['a protocol-relative form of this origin', '//app.altune.example/auth/callback?code=x'],
    [
      'a javascript: url naming the auth path',
      'javascript:alert(1)//app.altune.example/auth/callback?code=x',
    ],
    [
      'a data: url naming the auth path',
      'data:text/html,https://app.altune.example/auth/callback?code=x',
    ],
  ])('ignores %s', (_case, url) => {
    expect(parseAuthLink(url)).toEqual({ kind: 'ignored' });
  });

  it.each([
    ['a path that extends an auth path', `${ORIGIN}/auth/callbackx?code=x`],
    ['a sub-path under an auth path', `${ORIGIN}/auth/callback/extra?code=x`],
    ['an auth path nested under another segment', `${ORIGIN}/evil/auth/callback?code=x`],
    ['the bare auth directory', `${ORIGIN}/auth/?code=x`],
    ['the site root', `${ORIGIN}/?code=x`],
    ['a prototype-property path', `${ORIGIN}/__proto__?code=x`],
  ])('ignores %s at this origin', (_case, url) => {
    expect(parseAuthLink(url)).toEqual({ kind: 'ignored' });
  });

  it('still applies the url length cap to a same-origin link', () => {
    expect(parseAuthLink(`${ORIGIN}/auth/callback?code=${'a'.repeat(5000)}`)).toEqual({
      kind: 'ignored',
    });
  });

  it('still rejects a same-origin link carrying a malformed percent-escape', () => {
    expect(parseAuthLink(`${ORIGIN}/auth/callback?code=%zz`)).toEqual({ kind: 'ignored' });
  });

  it('follows the origin the page is on now, not the one it was first asked about', () => {
    expect(authRedirectUrl('callback')).toBe(`${ORIGIN}/auth/callback`);
    Object.assign(globalThis, {
      window: { location: { origin: 'https://staging.altune.example' } },
    });

    expect(authRedirectUrl('callback')).toBe('https://staging.altune.example/auth/callback');
    expect(parseAuthLink(`${ORIGIN}/auth/callback?code=x`)).toEqual({ kind: 'ignored' });
  });
});

describe('authRedirectUrl on native (#2837)', () => {
  it('returns the altune scheme url for every intent', () => {
    expect(authRedirectUrl('callback')).toBe('altune://auth/callback');
    expect(authRedirectUrl('confirm')).toBe('altune://auth/confirm');
    expect(authRedirectUrl('recovery')).toBe('altune://auth/recovery');
  });
});
