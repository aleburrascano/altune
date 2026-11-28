import appJson from '@/../app.json';
import { schemePrefix } from '@features/signIn/core/appScheme';
import { authRedirectUrl } from '@features/signIn/core/authRedirect';
import { parseAuthLink } from '@features/signIn/core/parseAuthLink';
import { completeAuthIntent } from '@features/signIn/__tests__/authTestUtils';
import type { AuthLinkHost } from '@features/signIn/core/authRedirect';

const ORIGIN = 'https://app.altune.example';
const WEB: AuthLinkHost = { scheme: 'altune://', origin: ORIGIN };
const OVER_URL_CAP = 5000;
const URL_CAP = 4096;
const PAIR_CAP = 64;
const QUERY_PAIRS = 32;
const FRAGMENT_PAIRS_TO_CAP = PAIR_CAP - QUERY_PAIRS;

function nativeHost(declared: string | string[]): AuthLinkHost {
  return { scheme: schemePrefix(declared), origin: null };
}

function numberedPairs(from: number, count: number): string {
  return Array.from({ length: count }, (_, i) => `k${from + i}=v`).join('&');
}

function numberedParams(count: number): Record<string, string> {
  return Object.fromEntries(Array.from({ length: count }, (_, i) => [`k${i}`, 'v']));
}

describe('parseAuthLink — the scheme comes from the expo config', () => {
  it("accepts app.json's configured scheme and builds its redirects from it", () => {
    const configured = appJson.expo.scheme;

    expect(parseAuthLink(`${configured}://auth/recovery?type=recovery`)).toEqual({
      kind: 'recovery',
      params: { type: 'recovery' },
    });
    expect(authRedirectUrl('callback')).toBe(`${configured}://auth/callback`);
    expect(authRedirectUrl('confirm')).toBe(`${configured}://auth/confirm`);
  });

  it('follows a rebranded scheme instead of the previous one', () => {
    const rebranded = nativeHost('whitelabel');

    expect(parseAuthLink('whitelabel://auth/recovery?type=recovery', rebranded)).toEqual({
      kind: 'recovery',
      params: { type: 'recovery' },
    });
    expect(parseAuthLink('altune://auth/recovery?type=recovery', rebranded)).toEqual({
      kind: 'ignored',
    });
    expect(authRedirectUrl('callback', rebranded)).toBe('whitelabel://auth/callback');
  });

  it('uses the first scheme when the config declares a list', () => {
    const multiScheme = nativeHost(['whitelabel', 'legacy']);

    expect(authRedirectUrl('callback', multiScheme)).toBe('whitelabel://auth/callback');
    expect(parseAuthLink('whitelabel://auth/callback?code=xyz', multiScheme)).toEqual({
      kind: 'oauth',
      params: { code: 'xyz' },
    });
  });

  it('matches a config scheme that is not lower-case', () => {
    const upperCased = nativeHost('WhiteLabel');

    expect(parseAuthLink('whitelabel://auth/callback?code=xyz', upperCased)).toEqual({
      kind: 'oauth',
      params: { code: 'xyz' },
    });
  });

  it.each([
    ['absent', undefined],
    ['an empty string', ''],
    ['an empty list', []],
  ])('throws naming `scheme` when the config declares %s', (_case, scheme) => {
    expect(() => schemePrefix(scheme)).toThrow('scheme');
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

  it('accepts 64 param pairs and rejects 65, well under the url length cap', () => {
    expect(parseAuthLink(`altune://auth/callback?${numberedPairs(0, PAIR_CAP)}`)).toEqual({
      kind: 'oauth',
      params: numberedParams(PAIR_CAP),
    });
    expect(parseAuthLink(`altune://auth/callback?${numberedPairs(0, PAIR_CAP + 1)}`)).toEqual({
      kind: 'ignored',
    });
  });

  it('counts the pair cap across the query and the fragment together', () => {
    const link = `altune://auth/callback?${numberedPairs(0, QUERY_PAIRS)}#`;

    expect(parseAuthLink(link + numberedPairs(QUERY_PAIRS, FRAGMENT_PAIRS_TO_CAP))).toEqual({
      kind: 'oauth',
      params: numberedParams(PAIR_CAP),
    });
    expect(parseAuthLink(link + numberedPairs(QUERY_PAIRS, FRAGMENT_PAIRS_TO_CAP + 1))).toEqual({
      kind: 'ignored',
    });
  });

  it('accepts a link of exactly the max url length and rejects one character more', () => {
    const prefix = 'altune://auth/callback?code=';
    const atCap = 'a'.repeat(URL_CAP - prefix.length);

    expect(parseAuthLink(prefix + atCap)).toEqual({ kind: 'oauth', params: { code: atCap } });
    expect(parseAuthLink(`${prefix}${atCap}a`)).toEqual({ kind: 'ignored' });
  });

  it('skips empty pairs without counting them as params', () => {
    expect(parseAuthLink('altune://auth/callback?&code=xyz&&#&state=s&')).toEqual({
      kind: 'oauth',
      params: { code: 'xyz', state: 's' },
    });
  });

  it('reads a key with no = as empty, an = in first place as an empty key, and splits on the first =', () => {
    expect(parseAuthLink('altune://auth/callback?flag&=x&code=a=b')).toEqual({
      kind: 'oauth',
      params: { flag: '', '': 'x', code: 'a=b' },
    });
  });

  it('strips any number of slashes around the auth path', () => {
    expect(parseAuthLink('altune:////auth/recovery//?type=recovery')).toEqual({
      kind: 'recovery',
      params: { type: 'recovery' },
    });
  });

  it('reads params from the fragment alone when there is no query', () => {
    expect(parseAuthLink('altune://auth/callback#code=xyz')).toEqual({
      kind: 'oauth',
      params: { code: 'xyz' },
    });
  });

  it('ends the query at a fragment that follows it', () => {
    expect(parseAuthLink('altune://auth/callback?code=a#state=b')).toEqual({
      kind: 'oauth',
      params: { code: 'a', state: 'b' },
    });
  });

  it('reads the query to the end when a fragment comes before it', () => {
    expect(parseAuthLink('altune://auth/callback#state=b?code=a')).toEqual({
      kind: 'oauth',
      params: { code: 'a', state: 'b?code=a' },
    });
  });

  it('rejects a malformed query even when the fragment after it is well formed', () => {
    expect(parseAuthLink('altune://auth/callback?code=%zz#state=ok')).toEqual({
      kind: 'ignored',
    });
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

describe('authRedirectUrl and parseAuthLink with a web origin (#2837)', () => {
  it('builds every redirect from this origin instead of the altune scheme', () => {
    expect(authRedirectUrl('callback', WEB)).toBe(`${ORIGIN}/auth/callback`);
    expect(authRedirectUrl('confirm', WEB)).toBe(`${ORIGIN}/auth/confirm`);
    expect(authRedirectUrl('recovery', WEB)).toBe(`${ORIGIN}/auth/recovery`);
  });

  it('keeps the altune scheme on native even with a global window present', () => {
    Object.assign(globalThis, { window: { location: { origin: ORIGIN } } });
    try {
      expect(authRedirectUrl('recovery')).toBe('altune://auth/recovery');
      expect(parseAuthLink(`${ORIGIN}/auth/recovery?type=recovery`)).toEqual({ kind: 'ignored' });
    } finally {
      Reflect.deleteProperty(globalThis, 'window');
    }
  });

  it('accepts this same origin for a known auth path', () => {
    expect(parseAuthLink(`${ORIGIN}/auth/recovery?token_hash=abc&type=recovery`, WEB)).toEqual({
      kind: 'recovery',
      params: { token_hash: 'abc', type: 'recovery' },
    });
  });

  it('still accepts the altune scheme alongside the web origin', () => {
    expect(parseAuthLink('altune://auth/callback?code=xyz', WEB)).toEqual({
      kind: 'oauth',
      params: { code: 'xyz' },
    });
  });

  it('still ignores a foreign origin on web', () => {
    expect(parseAuthLink('https://evil.example/auth/recovery?type=recovery', WEB)).toEqual({
      kind: 'ignored',
    });
  });

  it('still ignores an unknown path at this same origin', () => {
    expect(parseAuthLink(`${ORIGIN}/somewhere-else?code=x`, WEB)).toEqual({ kind: 'ignored' });
  });

  it('falls back to the altune scheme when there is no origin to ask', () => {
    expect(authRedirectUrl('confirm', { scheme: 'altune://', origin: null })).toBe(
      'altune://auth/confirm',
    );
  });

  it('ignores a foreign string merely because it is at least as long as this origin', () => {
    const short: AuthLinkHost = { scheme: 'altune://', origin: 'https://x.test' };
    const originPrefixLength = 'https://x.test/'.length;
    const foreign = 'a'.repeat(originPrefixLength) + 'auth/recovery?type=recovery';

    expect(parseAuthLink(foreign, short)).toEqual({ kind: 'ignored' });
  });

  it('ignores a bare path carrying neither the altune scheme nor a web origin', () => {
    expect(parseAuthLink('auth/recovery?type=recovery')).toEqual({ kind: 'ignored' });
    expect(parseAuthLink('auth/recovery?type=recovery', WEB)).toEqual({ kind: 'ignored' });
  });

  it('builds and matches against whichever origin it is handed', () => {
    const staging: AuthLinkHost = { scheme: 'altune://', origin: 'https://staging.altune.example' };

    expect(authRedirectUrl('callback', WEB)).toBe(`${ORIGIN}/auth/callback`);
    expect(authRedirectUrl('callback', staging)).toBe(
      'https://staging.altune.example/auth/callback',
    );
    expect(parseAuthLink(`${ORIGIN}/auth/callback?code=x`, staging)).toEqual({ kind: 'ignored' });
  });
});

describe('parseAuthLink with a web origin: only this exact origin and the three auth paths (#2837)', () => {
  it('accepts a same-origin oauth callback and a same-origin confirmation', () => {
    expect(parseAuthLink(`${ORIGIN}/auth/callback?code=xyz`, WEB)).toEqual({
      kind: 'oauth',
      params: { code: 'xyz' },
    });
    expect(parseAuthLink(`${ORIGIN}/auth/confirm?token_hash=abc&type=signup`, WEB)).toEqual({
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
    expect(parseAuthLink(url, WEB)).toEqual({ kind: 'ignored' });
  });

  it.each([
    ['a path that extends an auth path', `${ORIGIN}/auth/callbackx?code=x`],
    ['a sub-path under an auth path', `${ORIGIN}/auth/callback/extra?code=x`],
    ['an auth path nested under another segment', `${ORIGIN}/evil/auth/callback?code=x`],
    ['the bare auth directory', `${ORIGIN}/auth/?code=x`],
    ['the site root', `${ORIGIN}/?code=x`],
    ['a prototype-property path', `${ORIGIN}/__proto__?code=x`],
  ])('ignores %s at this origin', (_case, url) => {
    expect(parseAuthLink(url, WEB)).toEqual({ kind: 'ignored' });
  });

  it('still applies the url length cap to a same-origin link', () => {
    expect(parseAuthLink(`${ORIGIN}/auth/callback?code=${'a'.repeat(OVER_URL_CAP)}`, WEB)).toEqual({
      kind: 'ignored',
    });
  });

  it('still rejects a same-origin link carrying a malformed percent-escape', () => {
    expect(parseAuthLink(`${ORIGIN}/auth/callback?code=%zz`, WEB)).toEqual({ kind: 'ignored' });
  });
});

describe('authRedirectUrl on native (#2837)', () => {
  it('returns the altune scheme url for every intent', () => {
    expect(authRedirectUrl('callback')).toBe('altune://auth/callback');
    expect(authRedirectUrl('confirm')).toBe('altune://auth/confirm');
    expect(authRedirectUrl('recovery')).toBe('altune://auth/recovery');
  });
});

describe.each([
  ['native', { scheme: 'altune://', origin: null }],
  ['web', WEB],
] as const)('implicit-grant tokens never become a session on %s', (_platform, host) => {
  it('refuses altune://auth/callback#access_token=x&refresh_token=y', async () => {
    const auth = {
      exchangeCodeForSession: jest.fn(),
      setSession: jest.fn(),
      verifyOtp: jest.fn(),
      getSession: jest.fn(),
    };
    const result = await completeAuthIntent(
      parseAuthLink('altune://auth/callback#access_token=x&refresh_token=y', host),
      { replace: jest.fn() },
      auth,
    );

    expect(result).toMatchObject({ kind: 'failure', cause: 'no_spendable_credential' });
    expect(auth.setSession).not.toHaveBeenCalled();
  });
});
