import { parseAuthLink } from '../parseAuthLink';

describe('parseAuthLink', () => {
  it('recognizes a recovery link with fragment tokens', () => {
    const intent = parseAuthLink(
      'altune://auth/recovery#access_token=abc&refresh_token=def&type=recovery',
    );
    expect(intent.kind).toBe('recovery');
    if (intent.kind === 'recovery') {
      expect(intent.params.access_token).toBe('abc');
      expect(intent.params.refresh_token).toBe('def');
    }
  });

  it('recognizes a recovery link with a query token_hash', () => {
    const intent = parseAuthLink('altune://auth/recovery?token_hash=xyz&type=recovery');
    expect(intent.kind).toBe('recovery');
    if (intent.kind === 'recovery') {
      expect(intent.params.token_hash).toBe('xyz');
    }
  });

  it('recognizes an email confirmation link', () => {
    const intent = parseAuthLink('altune://auth/confirm?token_hash=tok&type=signup');
    expect(intent.kind).toBe('confirm');
    if (intent.kind === 'confirm') {
      expect(intent.params.token_hash).toBe('tok');
      expect(intent.params.type).toBe('signup');
    }
  });

  it('recognizes an oauth callback with a PKCE code', () => {
    const intent = parseAuthLink('altune://auth/callback?code=authcode123');
    expect(intent.kind).toBe('oauth');
    if (intent.kind === 'oauth') {
      expect(intent.params.code).toBe('authcode123');
    }
  });

  it('merges query and fragment params', () => {
    const intent = parseAuthLink('altune://auth/callback?code=c1#extra=e1');
    expect(intent.kind).toBe('oauth');
    if (intent.kind === 'oauth') {
      expect(intent.params.code).toBe('c1');
      expect(intent.params.extra).toBe('e1');
    }
  });

  it.each([
    'altune://library/123',
    'altune://auth/unknown?x=1',
    'altune://',
    'https://evil.com/auth/recovery#access_token=abc',
    '',
    'garbage',
  ])('ignores non-whitelisted or foreign links: %s', (url) => {
    expect(parseAuthLink(url).kind).toBe('ignored');
  });

  it('url-decodes param values', () => {
    const intent = parseAuthLink('altune://auth/callback?code=a%20b');
    if (intent.kind === 'oauth') {
      expect(intent.params.code).toBe('a b');
    }
  });
});
