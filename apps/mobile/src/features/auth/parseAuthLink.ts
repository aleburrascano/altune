import Constants from 'expo-constants';
import { Platform } from 'react-native';

// The scheme belongs to the app, not to this module: app.json's `scheme` is what
// the OS registers and what Supabase redirects to, so a second literal here could
// drift from it and silently ignore every real auth link (#1649). An absent scheme
// throws at import — a dead launch, like supabaseClient's missing credentials,
// beats a dead password-reset tap. Expo permits a list, and its first entry is the
// only scheme we hand out as a redirect, so it is the only one links return on.
function configuredSchemePrefix(): string {
  const declared = Constants.expoConfig?.scheme;
  const primary = Array.isArray(declared) ? declared[0] : declared;
  if (typeof primary !== 'string' || primary === '') {
    throw new Error('Missing required Expo config field `scheme` (apps/mobile/app.json)');
  }
  // Lower-cased once here; the prefix match below compares lower-cased text.
  return `${primary.toLowerCase()}://`;
}

const SCHEME = configuredSchemePrefix();

// Bounds: an external, unverified deep link is parsed synchronously on the JS
// thread, so cap the total link length and the number of param pairs we will
// walk before giving up. These are generous relative to any real auth link.
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

// Object.create(null) has no prototype, so inherited names like `__proto__`
// cannot resolve to a value and bypass the "unknown path" guard.
const PATH_TO_KIND: Record<string, SpendableLinkKind> = Object.assign(Object.create(null), {
  [LINK_PATH.recovery]: 'recovery',
  [LINK_PATH.confirm]: 'confirm',
  [LINK_PATH.oauth]: 'oauth',
});

// Redirect URLs handed to Supabase must round-trip back through the deep-link
// paths above, so derive them from the same scheme + path vocabulary rather
// than restating the literals in each hook.
export const OAUTH_REDIRECT_URL = `${SCHEME}${LINK_PATH.oauth}`;
export const CONFIRM_REDIRECT_URL = `${SCHEME}${LINK_PATH.confirm}`;
export const RECOVERY_REDIRECT_URL = `${SCHEME}${LINK_PATH.recovery}`;

export type AuthRedirectIntent = 'callback' | 'confirm' | 'recovery';

const REDIRECT_PATH_FOR_INTENT: Record<AuthRedirectIntent, string> = {
  callback: LINK_PATH.oauth,
  confirm: LINK_PATH.confirm,
  recovery: LINK_PATH.recovery,
};

function webOrigin(): string | null {
  if (Platform.OS !== 'web' || typeof window === 'undefined') {
    return null;
  }
  return window.location?.origin || null;
}

export function authRedirectUrl(intent: AuthRedirectIntent): string {
  const path = REDIRECT_PATH_FOR_INTENT[intent];
  const origin = webOrigin();
  return origin ? `${origin}/${path}` : `${SCHEME}${path}`;
}

// The in-app route a verified recovery exchange lands on. Its two uses have
// unequal protection: `router.replace` takes a typed `Href`, while AuthGate
// compares against `useSegments()`'s plain `string[]`. Sharing one name means a
// route rename surfaces at the typed site instead of silently rotting the
// untyped guard that keeps bare deep links off the reset form (#656).
export const RESET_PASSWORD_ROUTE_SEGMENT = 'reset-password';

function lookupKind(path: string): SpendableLinkKind | undefined {
  if (!Object.prototype.hasOwnProperty.call(PATH_TO_KIND, path)) {
    return undefined;
  }
  return PATH_TO_KIND[path];
}

// Returns false once the running pair count exceeds the cap, so the caller can
// abandon parsing instead of walking an unbounded `&`-separated segment.
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

// A percent-escape decodeURIComponent refuses — a truncated `%4`, a non-hex
// `%zz`, an escape sequence spelling ill-formed UTF-8 — has no decoded value:
// the raw text is not what the sender wrote, so it is absent, not a fallback.
function decodeComponent(raw: string): string | undefined {
  try {
    return decodeURIComponent(raw);
  } catch {
    return undefined;
  }
}

// False rejects the whole link rather than dropping the offending pair: a
// dropped param silently re-steers the caller — a genuine recovery link that
// loses its `type` reads as a link with nothing spendable on it — and a link
// truncated in transit must not burn the dedupe slot its intact retry needs.
// `ignored` also keeps "corrupted in transit" distinct from "the server
// rejected it" (#1645).
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
