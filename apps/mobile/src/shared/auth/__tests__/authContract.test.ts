import * as fs from 'fs';
import * as path from 'path';

import { apiFetch } from '@shared/api-client';
import { supabase } from '@shared/auth/supabaseClient';
import { clearSessionExpired, getSessionExpired } from '@shared/auth/sessionExpired';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const getSession = supabase.auth.getSession as jest.Mock;

function withSession(accessToken = 'contract-fixture-token') {
  getSession.mockResolvedValue({
    data: { session: { access_token: accessToken } },
    error: null,
  });
}

beforeEach(() => {
  clearSessionExpired();
  getSession.mockReset();
  __http.reset();
});

function findRepoRoot(): string | null {
  let dir = process.cwd();
  for (let hop = 0; hop < 8; hop += 1) {
    const hasGoApi = fs.existsSync(path.join(dir, 'services', 'go-api'));
    const hasMobile = fs.existsSync(path.join(dir, 'apps', 'mobile'));
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
  const re = new RegExp(`\\nfunc ${funcName}\\([^)]*\\)[^{]*\\{`);
  const m = re.exec(source);
  if (!m) throw new Error(`func ${funcName} not found in middleware.go`);
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

function middlewareSource(): string {
  return fs.readFileSync(
    path.join(REPO_ROOT!, 'services', 'go-api', 'internal', 'auth', 'middleware.go'),
    'utf8',
  );
}

function apiClientSource(): string {
  return fs.readFileSync(
    path.join(REPO_ROOT!, 'apps', 'mobile', 'src', 'shared', 'api-client', 'index.ts'),
    'utf8',
  );
}

function deriveRejectTokenStatus(source: string): number {
  const body = extractGoFuncBody(source, 'rejectToken');
  const match = /httputil\.WriteJSON\(w, (http\.Status\w+),/.exec(body);
  expect(match).not.toBeNull();
  const status = GO_HTTP_STATUS_CONSTANTS[match![1]!];
  expect(status).toBeDefined();
  return status!;
}

function deriveVerifierUnavailableStatus(source: string): number {
  const body = extractGoFuncBody(source, 'rejectVerifierUnavailable');
  const match = /httputil\.WriteError\(w, (http\.Status\w+),/.exec(body);
  expect(match).not.toBeNull();
  const status = GO_HTTP_STATUS_CONSTANTS[match![1]!];
  expect(status).toBeDefined();
  return status!;
}

function deriveMobileSessionExpiredStatus(source: string): number {
  const match = /if \(response\.status === (\d+)\) markSessionExpired\(\);/.exec(source);
  expect(match).not.toBeNull();
  return Number(match![1]);
}

describe('cross-surface 401 contract, derived from services/go-api/internal/auth/middleware.go at test time', () => {
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

    expect(rejectTokenBody).toContain('w.Header().Set("WWW-Authenticate", "Bearer")');
    expect(rejectVerifierUnavailableBody).not.toContain('WWW-Authenticate');
  });
});

describe('runtime: apiFetch agrees with the derived contract', () => {
  it('marks the session expired when the server answers with the derived rejectToken status (a rejected/expired/malformed token)', async () => {
    const rejectTokenStatus = deriveRejectTokenStatus(middlewareSource());
    withSession();
    __http.reply('GET /v1/library/tracks', {
      status: rejectTokenStatus,
      json: { detail: 'invalid token', reason: 'expired' },
    });

    await expect(apiFetch('/v1/library/tracks')).rejects.toMatchObject({ status: rejectTokenStatus });
    expect(getSessionExpired()).toBe(true);
  });

  it('does NOT mark the session expired when the server answers with the derived verifier-unavailable status (a JWKS outage bouncing every user mid-session would be a production incident)', async () => {
    const verifierUnavailableStatus = deriveVerifierUnavailableStatus(middlewareSource());
    withSession();
    __http.reply('GET /v1/library/tracks', {
      status: verifierUnavailableStatus,
      json: { detail: 'authentication unavailable' },
    });

    await expect(apiFetch('/v1/library/tracks')).rejects.toMatchObject({
      status: verifierUnavailableStatus,
    });
    expect(getSessionExpired()).toBe(false);
  });
});
