import { declaredAppScheme, webOrigin } from '@shared/device/device';

function configuredSchemePrefix(): string {
  const declared = declaredAppScheme();
  const primary = Array.isArray(declared) ? declared[0] : declared;
  if (typeof primary !== 'string' || primary === '') {
    throw new Error('Missing required Expo config field `scheme` (apps/mobile/app.json)');
  }
  return `${primary.toLowerCase()}://`;
}

const SCHEME = configuredSchemePrefix();

const MAX_URL_LENGTH = 4096;
const MAX_PARAM_PAIRS = 64;

export type AuthLinkParams = Record<string, string>;

export type AuthLinkIntent =
  | { kind: 'recovery'; params: AuthLinkParams }
  | { kind: 'confirm'; params: AuthLinkParams }
  | { kind: 'oauth'; params: AuthLinkParams }
  | { kind: 'ignored' };

const LINK_PATH = {
  recovery: 'auth/recovery',
  confirm: 'auth/confirm',
  oauth: 'auth/callback',
} as const;

type SpendableLinkKind = Exclude<AuthLinkIntent['kind'], 'ignored'>;

const PATH_TO_KIND: Record<string, SpendableLinkKind> = Object.assign(Object.create(null), {
  [LINK_PATH.recovery]: 'recovery',
  [LINK_PATH.confirm]: 'confirm',
  [LINK_PATH.oauth]: 'oauth',
});

export const OAUTH_REDIRECT_URL = `${SCHEME}${LINK_PATH.oauth}`;
export const CONFIRM_REDIRECT_URL = `${SCHEME}${LINK_PATH.confirm}`;
export const RECOVERY_REDIRECT_URL = `${SCHEME}${LINK_PATH.recovery}`;

export type AuthRedirectIntent = 'callback' | 'confirm' | 'recovery';

const REDIRECT_PATH_FOR_INTENT: Record<AuthRedirectIntent, string> = {
  callback: LINK_PATH.oauth,
  confirm: LINK_PATH.confirm,
  recovery: LINK_PATH.recovery,
};

export function authRedirectUrl(intent: AuthRedirectIntent): string {
  const path = REDIRECT_PATH_FOR_INTENT[intent];
  const origin = webOrigin();
  return origin ? `${origin}/${path}` : `${SCHEME}${path}`;
}

export const RESET_PASSWORD_ROUTE_SEGMENT = 'reset-password';

function lookupKind(path: string): SpendableLinkKind | undefined {
  if (!Object.prototype.hasOwnProperty.call(PATH_TO_KIND, path)) {
    return undefined;
  }
  return PATH_TO_KIND[path];
}

function parseParamSegment(segment: string, into: AuthLinkParams, seen: number): number | false {
  let count = seen;
  for (const pair of segment.split('&')) {
    if (!pair) {
      continue;
    }
    count += 1;
    if (count > MAX_PARAM_PAIRS) {
      return false;
    }
    if (!assignPair(pair, into)) {
      return false;
    }
  }
  return count;
}

function decodeComponent(raw: string): string | undefined {
  try {
    return decodeURIComponent(raw);
  } catch {
    return undefined;
  }
}

function assignPair(pair: string, into: AuthLinkParams): boolean {
  const eq = pair.indexOf('=');
  const key = decodeComponent(eq >= 0 ? pair.slice(0, eq) : pair);
  const value = decodeComponent(eq >= 0 ? pair.slice(eq + 1) : '');
  if (key === undefined || value === undefined) {
    return false;
  }
  into[key] = value;
  return true;
}

function matchedPrefixLength(url: string): number | null {
  if (url.slice(0, SCHEME.length).toLowerCase() === SCHEME) {
    return SCHEME.length;
  }
  const origin = webOrigin();
  const originPrefix = origin ? `${origin}/` : null;
  return originPrefix && url.startsWith(originPrefix) ? originPrefix.length : null;
}

export function parseAuthLink(url: string): AuthLinkIntent {
  if (url.length > MAX_URL_LENGTH) {
    return { kind: 'ignored' };
  }
  const prefixLength = matchedPrefixLength(url);
  if (prefixLength === null) {
    return { kind: 'ignored' };
  }

  const rest = url.slice(prefixLength);
  const hashIdx = rest.indexOf('#');
  const queryIdx = rest.indexOf('?');

  const pathEnd = Math.min(
    queryIdx === -1 ? rest.length : queryIdx,
    hashIdx === -1 ? rest.length : hashIdx,
  );
  const path = rest.slice(0, pathEnd).replace(/^\/+|\/+$/g, '');

  const kind = lookupKind(path);
  if (!kind) {
    return { kind: 'ignored' };
  }

  const params: AuthLinkParams = {};
  const query =
    queryIdx !== -1
      ? rest.slice(queryIdx + 1, hashIdx !== -1 && hashIdx > queryIdx ? hashIdx : undefined)
      : '';
  const fragment = hashIdx !== -1 ? rest.slice(hashIdx + 1) : '';

  let seen: number | false = 0;
  if (query) {
    seen = parseParamSegment(query, params, seen);
  }
  if (seen !== false && fragment) {
    seen = parseParamSegment(fragment, params, seen);
  }
  if (seen === false) {
    return { kind: 'ignored' };
  }

  return { kind, params };
}
