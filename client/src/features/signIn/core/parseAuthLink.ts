import { appLinkHost, type AuthLinkHost, LINK_PATH } from '@features/signIn/core/authRedirect';
import { isPresent } from '@features/signIn/core/isPresent';

const MAX_URL_LENGTH = 4096;
const MAX_PARAM_PAIRS = 64;
const LEADING_OR_TRAILING_SLASHES = /^\/+|\/+$/g;

export type AuthLinkParams = Record<string, string>;

export type AuthLinkIntent =
  | { kind: 'recovery'; params: AuthLinkParams }
  | { kind: 'confirm'; params: AuthLinkParams }
  | { kind: 'oauth'; params: AuthLinkParams }
  | { kind: 'ignored' };

type SpendableLinkKind = Exclude<AuthLinkIntent['kind'], 'ignored'>;

const PATH_TO_KIND: Record<string, SpendableLinkKind> = Object.assign(Object.create(null), {
  [LINK_PATH.recovery]: 'recovery',
  [LINK_PATH.confirm]: 'confirm',
  [LINK_PATH.oauth]: 'oauth',
});

function lookupKind(path: string): SpendableLinkKind | undefined {
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

function decodeComponent(raw: string): string | null {
  try {
    return decodeURIComponent(raw);
  } catch {
    return null;
  }
}

function assignPair(pair: string, into: AuthLinkParams): boolean {
  const eq = pair.indexOf('=');
  const key = decodeComponent(eq >= 0 ? pair.slice(0, eq) : pair);
  const value = decodeComponent(eq >= 0 ? pair.slice(eq + 1) : '');
  if (key === null || value === null) {
    return false;
  }
  into[key] = value;
  return true;
}

function matchedPrefixLength(url: string, { scheme, origin }: AuthLinkHost): number | null {
  if (url.slice(0, scheme.length).toLowerCase() === scheme) {
    return scheme.length;
  }
  const originPrefix = isPresent(origin) ? `${origin}/` : null;
  return originPrefix !== null && url.startsWith(originPrefix) ? originPrefix.length : null;
}

function endOrLength(index: number, length: number): number {
  return index === -1 ? length : index;
}

function pathEnd(rest: string, queryIdx: number, hashIdx: number): number {
  return Math.min(endOrLength(queryIdx, rest.length), endOrLength(hashIdx, rest.length));
}

function linkPath(rest: string, queryIdx: number, hashIdx: number): string {
  return rest.slice(0, pathEnd(rest, queryIdx, hashIdx)).replace(LEADING_OR_TRAILING_SLASHES, '');
}

function queryEnd(rest: string, queryIdx: number, hashIdx: number): number {
  const noHashBeforeQuery = pathEnd(rest, queryIdx, hashIdx) === queryIdx;
  return noHashBeforeQuery ? endOrLength(hashIdx, rest.length) : rest.length;
}

function querySegment(rest: string, queryIdx: number, hashIdx: number): string {
  if (queryIdx === -1) return '';
  return rest.slice(queryIdx + 1, queryEnd(rest, queryIdx, hashIdx));
}

function fragmentSegment(rest: string, hashIdx: number): string {
  return hashIdx !== -1 ? rest.slice(hashIdx + 1) : '';
}

function linkParams(query: string, fragment: string): AuthLinkParams | null {
  const params: AuthLinkParams = {};
  const seen = parseParamSegment(query, params, 0);
  if (seen === false) return null;
  return parseParamSegment(fragment, params, seen) === false ? null : params;
}

function linkRest(url: string, host: AuthLinkHost): string | null {
  if (url.length > MAX_URL_LENGTH) return null;
  const prefixLength = matchedPrefixLength(url, host);
  return prefixLength === null ? null : url.slice(prefixLength);
}

function intentFor(rest: string): AuthLinkIntent {
  const hashIdx = rest.indexOf('#');
  const queryIdx = rest.indexOf('?');
  const kind = lookupKind(linkPath(rest, queryIdx, hashIdx));
  if (!kind) return { kind: 'ignored' };
  const params = linkParams(querySegment(rest, queryIdx, hashIdx), fragmentSegment(rest, hashIdx));
  return params ? { kind, params } : { kind: 'ignored' };
}

export function parseAuthLink(url: string, host: AuthLinkHost = appLinkHost()): AuthLinkIntent {
  const rest = linkRest(url, host);
  return rest === null ? { kind: 'ignored' } : intentFor(rest);
}
