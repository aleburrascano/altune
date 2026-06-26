/**
 * parseAuthLink — pure classifier for incoming `altune://` auth deep links.
 *
 * The single deep-link spine (email-confirm, password-recovery, OAuth) routes
 * through this. Per rn-security.md, only whitelisted callback paths are acted
 * on; anything else — foreign scheme, unknown path, garbage — is `ignored`.
 * Token handling lives in the handler/SDK; this only classifies + extracts.
 *
 * AIDEV-NOTE: exact param names (token_hash vs access_token, code) depend on
 * the Supabase email-template + PKCE-vs-implicit config. The whitelist is by
 * PATH, so it's robust to which token style the dashboard ends up using; the
 * handler reads whichever params are present.
 */
const SCHEME = 'altune://';

export type AuthLinkParams = Record<string, string>;

export type AuthLinkIntent =
  | { kind: 'recovery'; params: AuthLinkParams }
  | { kind: 'confirm'; params: AuthLinkParams }
  | { kind: 'oauth'; params: AuthLinkParams }
  | { kind: 'ignored' };

const PATH_TO_KIND: Record<string, 'recovery' | 'confirm' | 'oauth'> = {
  'auth/recovery': 'recovery',
  'auth/confirm': 'confirm',
  'auth/callback': 'oauth',
};

function parseParamSegment(segment: string, into: AuthLinkParams): void {
  for (const pair of segment.split('&')) {
    if (!pair) {
      continue;
    }
    const eq = pair.indexOf('=');
    const rawKey = eq >= 0 ? pair.slice(0, eq) : pair;
    const rawVal = eq >= 0 ? pair.slice(eq + 1) : '';
    try {
      into[decodeURIComponent(rawKey)] = decodeURIComponent(rawVal);
    } catch {
      into[rawKey] = rawVal;
    }
  }
}

export function parseAuthLink(url: string): AuthLinkIntent {
  if (!url.startsWith(SCHEME)) {
    return { kind: 'ignored' };
  }

  const rest = url.slice(SCHEME.length);
  const hashIdx = rest.indexOf('#');
  const queryIdx = rest.indexOf('?');

  const pathEnd = Math.min(
    queryIdx === -1 ? rest.length : queryIdx,
    hashIdx === -1 ? rest.length : hashIdx,
  );
  const path = rest.slice(0, pathEnd).replace(/^\/+|\/+$/g, '');

  const kind = PATH_TO_KIND[path];
  if (!kind) {
    return { kind: 'ignored' };
  }

  const params: AuthLinkParams = {};
  const query =
    queryIdx !== -1
      ? rest.slice(queryIdx + 1, hashIdx !== -1 && hashIdx > queryIdx ? hashIdx : undefined)
      : '';
  const fragment = hashIdx !== -1 ? rest.slice(hashIdx + 1) : '';
  if (query) {
    parseParamSegment(query, params);
  }
  if (fragment) {
    parseParamSegment(fragment, params);
  }

  return { kind, params };
}
