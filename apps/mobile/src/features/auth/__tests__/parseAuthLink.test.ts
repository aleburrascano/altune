import { parseAuthLink } from '../parseAuthLink';

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
