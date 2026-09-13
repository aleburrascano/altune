import { parseAuthLink } from '../lib/parseAuthLink';

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
});
