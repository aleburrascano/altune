import * as fs from 'fs';
import * as path from 'path';

import { apiFetch } from '@shared/apiClient';
import { clearSessionExpired, getSessionExpired } from '@shared/auth';
import {
  installSessionLookup,
  type SessionLookup,
} from '@shared/apiClient/__tests__/sessionLookup';
const { __http } = require('@testing/doubles/fetch.js');

let getSession: SessionLookup;

function withSession(accessToken = 'contract-fixture-token') {
  getSession.mockResolvedValue({
    data: { session: { access_token: accessToken } },
    error: null,
  });
}

beforeEach(() => {
  clearSessionExpired();
  getSession = installSessionLookup();
  __http.reset();
});

function findRepoRoot(): string | null {
  let dir = process.cwd();
  for (let hop = 0; hop < 8; hop += 1) {
    const hasGoApi = fs.existsSync(path.join(dir, 'services', 'go-api'));
    const hasMobile = fs.existsSync(path.join(dir, 'client'));
    if (hasGoApi && hasMobile) return dir;
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return null;
}

const REPO_ROOT = findRepoRoot();

function findMatchingBrace(text: string, openIndex: number): number {
  let depth = 0;
  for (let i = openIndex; i < text.length; i += 1) {
    if (text[i] === '{') depth += 1;
    else if (text[i] === '}') {
      depth -= 1;
      if (depth === 0) return i;
    }
  }
  throw new Error(`unbalanced braces starting at ${openIndex}`);
}

function extractGoFuncBody(source: string, funcName: string): string {
  const plain = new RegExp(`\\nfunc ${funcName}\\([^)]*\\)[^{]*\\{`);
  const method = new RegExp(`\\nfunc \\([^)]*\\) ${funcName}\\([^)]*\\)[^{]*\\{`);
  const m = plain.exec(source) ?? method.exec(source);
  if (!m) throw new Error(`func ${funcName} not found in the auth middleware source`);
  const braceIdx = m.index + m[0].length - 1;
  const closeIdx = findMatchingBrace(source, braceIdx);
  return source.slice(braceIdx + 1, closeIdx);
}

const GO_HTTP_STATUS_CONSTANTS: Record<string, number> = {
  'http.StatusOK': 200,
  'http.StatusBadRequest': 400,
  'http.StatusUnauthorized': 401,
  'http.StatusForbidden': 403,
  'http.StatusNotFound': 404,
  'http.StatusConflict': 409,
  'http.StatusInternalServerError': 500,
  'http.StatusServiceUnavailable': 503,
};

const MIDDLEWARE_MARKER = /\nfunc (\([^)]*\) )?rejectFailedVerification\(/;

function goSourcesUnder(dir: string): string[] {
  return fs
    .readdirSync(dir, { encoding: 'utf8', recursive: true })
    .filter((file) => file.endsWith('.go') && !file.endsWith('_test.go'))
    .map((file) => path.join(dir, file));
}

let foundMiddlewareSource: string | undefined;

function middlewareSource(): string {
  foundMiddlewareSource ??= findMiddlewareSource();
  return foundMiddlewareSource;
}

function findMiddlewareSource(): string {
  const authModule = path.join(REPO_ROOT!, 'services', 'go-api', 'internal', 'platform', 'auth');
  const sources = goSourcesUnder(authModule)
    .map((file) => fs.readFileSync(file, 'utf8'))
    .filter((source) => MIDDLEWARE_MARKER.test(source));
  const [source, ...others] = sources;
  if (source === undefined || others.length > 0) {
    throw new Error(
      `expected one Go file under ${authModule} declaring rejectFailedVerification, found ${sources.length}`,
    );
  }
  return source;
}

function apiClientSource(): string {
  return fs.readFileSync(
    path.join(REPO_ROOT!, 'client', 'src', 'shared', 'apiClient', 'adapters', 'http.ts'),
    'utf8',
  );
}

const REJECTION_CODES_DECLARATION = /\nvar rejectionCodes = [^{]*\{/;
const RETURNED_DOMAIN_CODE = /return apierror\.(Code\w+)/g;
const DOMAIN_CODE_REFERENCE = /apierror\.(Code\w+)/g;
const REJECTION_REASON_KEY = /ports\.Reason\w+:/g;
const NEW_ERROR_CODE_ARGUMENT = /apierror\.NewError\(apierror\.(Code\w+),/g;

function goApiSource(...segments: string[]): string {
  return fs.readFileSync(
    path.join(REPO_ROOT!, 'services', 'go-api', 'internal', ...segments),
    'utf8',
  );
}

function statusOfCode(codeName: string): number {
  const kind = new RegExp(`\\b${codeName}\\s*=\\s*code\\("[^"]+",\\s*(Kind\\w+),`).exec(
    goApiSource('shared', 'apierror', 'codes.go'),
  );
  expect(kind).not.toBeNull();
  const status = new RegExp(`\\bapierror\\.${kind![1]}:\\s*(http\\.Status\\w+),`).exec(
    goApiSource('shared', 'httpserver', 'errors.go'),
  );
  expect(status).not.toBeNull();
  const value = GO_HTTP_STATUS_CONSTANTS[status![1]!];
  expect(value).toEqual(expect.any(Number));
  return value!;
}

function rejectionCodeNames(source: string): string[] {
  const declaration = REJECTION_CODES_DECLARATION.exec(source);
  expect(declaration).not.toBeNull();
  const open = declaration!.index + declaration![0].length - 1;
  const table = source.slice(open, findMatchingBrace(source, open));
  const tableCodes = [...table.matchAll(DOMAIN_CODE_REFERENCE)].map((m) => m[1]!);
  expect(tableCodes).toHaveLength([...table.matchAll(REJECTION_REASON_KEY)].length);
  const fallbacks = [
    ...extractGoFuncBody(source, 'rejectionCode').matchAll(RETURNED_DOMAIN_CODE),
  ].map((m) => m[1]!);
  expect(fallbacks.length).toBeGreaterThan(0);
  return [...tableCodes, ...fallbacks];
}

function soleStatusOf(codeNames: string[]): number {
  const statuses = new Set(codeNames.map(statusOfCode));
  expect(statuses.size).toBe(1);
  return [...statuses][0]!;
}

function deriveRejectTokenStatus(source: string): number {
  expect(extractGoFuncBody(source, 'rejectToken')).toContain(
    'httpserver.HandleServiceError(w, r, apierror.NewError(rejectionCode(reason),',
  );
  return soleStatusOf(rejectionCodeNames(source));
}

function deriveVerifierUnavailableStatus(source: string): number {
  const body = extractGoFuncBody(source, 'rejectVerifierUnavailable');
  expect(body).toContain('httpserver.HandleServiceError(w, r,');
  const codes = [...body.matchAll(NEW_ERROR_CODE_ARGUMENT)].map((m) => m[1]!);
  expect(codes.length).toBeGreaterThan(0);
  return soleStatusOf(codes);
}

function deriveMobileSessionExpiredStatus(source: string): number {
  const match = /if \(response\.status === (\w+)\) markSessionExpired\(sentWith\);/.exec(source);
  expect(match).not.toBeNull();
  const errorsSource = fs.readFileSync(
    path.join(REPO_ROOT!, 'client', 'src', 'shared', 'failure', 'core', 'errors.ts'),
    'utf8',
  );
  const constant = new RegExp(`export const ${match![1]} = (\\d+);`).exec(errorsSource);
  expect(constant).not.toBeNull();
  return Number(constant![1]);
}

describe('cross-surface 401 contract, derived from the auth middleware under services/go-api/internal/platform/auth at test time', () => {
  it('finds the repo root to derive the contract from', () => {
    expect(REPO_ROOT).not.toBeNull();
  });

  it('rejectFailedVerification routes only an *InvalidTokenError to rejectToken; every other verifier failure goes to rejectVerifierUnavailable', () => {
    const body = extractGoFuncBody(middlewareSource(), 'rejectFailedVerification');

    expect(body).toContain('errors.As(err, &invalidToken)');
    const tokenCallIdx = body.indexOf('rejectToken(');
    const unavailableCallIdx = body.indexOf('rejectVerifierUnavailable(');
    expect(tokenCallIdx).toBeGreaterThan(-1);
    expect(unavailableCallIdx).toBeGreaterThan(-1);
    expect(tokenCallIdx).toBeLessThan(unavailableCallIdx);
  });

  it('rejectToken writes exactly the status the mobile client treats as "session expired"', () => {
    const rejectTokenStatus = deriveRejectTokenStatus(middlewareSource());
    const mobileCheckedStatus = deriveMobileSessionExpiredStatus(apiClientSource());

    expect(mobileCheckedStatus).toBe(rejectTokenStatus);
  });

  it('rejectVerifierUnavailable (the verifier itself could not run) writes a status distinct from the one the mobile client treats as "session expired" — a JWKS outage must not mass-expire every session', () => {
    const verifierUnavailableStatus = deriveVerifierUnavailableStatus(middlewareSource());
    const mobileCheckedStatus = deriveMobileSessionExpiredStatus(apiClientSource());

    expect(verifierUnavailableStatus).not.toBe(mobileCheckedStatus);
  });

  it('rejectToken sets the WWW-Authenticate challenge header; rejectVerifierUnavailable does not', () => {
    const source = middlewareSource();
    const rejectTokenBody = extractGoFuncBody(source, 'rejectToken');
    const rejectVerifierUnavailableBody = extractGoFuncBody(source, 'rejectVerifierUnavailable');

    const challengeIdx = rejectTokenBody.indexOf('w.Header().Set("WWW-Authenticate", "Bearer")');
    expect(challengeIdx).toBeGreaterThan(-1);
    expect(challengeIdx).toBeLessThan(rejectTokenBody.indexOf('httpserver.HandleServiceError('));
    expect(rejectVerifierUnavailableBody).not.toContain('WWW-Authenticate');
  });
});

describe('runtime: apiFetch agrees with the derived contract', () => {
  it('marks the session expired when the server answers with the derived rejectToken status (a rejected/expired/malformed token)', async () => {
    const rejectTokenStatus = deriveRejectTokenStatus(middlewareSource());
    withSession();
    __http.reply('GET /v1/library/tracks', {
      status: rejectTokenStatus,
      json: { detail: 'invalid token', code: 'auth.token_expired' },
    });

    await expect(apiFetch('/v1/library/tracks')).rejects.toMatchObject({
      status: rejectTokenStatus,
    });
    expect(getSessionExpired()).toBe(true);
  });

  it('does NOT mark the session expired when the server answers with the derived verifier-unavailable status (a JWKS outage bouncing every user mid-session would be a production incident)', async () => {
    const verifierUnavailableStatus = deriveVerifierUnavailableStatus(middlewareSource());
    withSession();
    __http.reply('GET /v1/library/tracks', {
      status: verifierUnavailableStatus,
      json: { detail: 'authentication unavailable', code: 'auth.unavailable' },
    });

    await expect(apiFetch('/v1/library/tracks')).rejects.toMatchObject({
      status: verifierUnavailableStatus,
    });
    expect(getSessionExpired()).toBe(false);
  });
});
