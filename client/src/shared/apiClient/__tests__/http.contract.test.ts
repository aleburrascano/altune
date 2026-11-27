import { apiFetch } from '@shared/apiClient';
import { clearSessionExpired, getSessionExpired } from '@shared/auth';
import {
  installSessionLookup,
  type SessionLookup,
} from '@shared/apiClient/__tests__/sessionLookup';
import { wireFixture } from '@testing/wireFixtures';
const { __http } = require('@testing/doubles/fetch.js');

type Rejection = { status: number; www_authenticate: string; body: { code: string } };

let getSession: SessionLookup;

beforeEach(() => {
  clearSessionExpired();
  getSession = installSessionLookup();
  getSession.mockResolvedValue({
    data: { session: { access_token: 'contract-fixture-token' } },
    error: null,
  });
  __http.reset();
});

function rejection(name: string): Rejection {
  return wireFixture(name) as Rejection;
}

describe('the 401 and 503 the Go auth middleware writes, read from its wire fixtures', () => {
  it('a rejected token carries a Bearer challenge and expires the session', async () => {
    const rejected = rejection('auth_token_rejected');
    __http.reply('GET /v1/tracks', { status: rejected.status, json: rejected.body });

    await expect(apiFetch('/v1/tracks')).rejects.toMatchObject({ status: rejected.status });

    expect(rejected.www_authenticate).toBe('Bearer');
    expect(getSessionExpired()).toBe(true);
  });

  it('an unavailable verifier carries no challenge and leaves the session alive', async () => {
    const unavailable = rejection('auth_verifier_unavailable');
    __http.reply('GET /v1/tracks', { status: unavailable.status, json: unavailable.body });

    await expect(apiFetch('/v1/tracks')).rejects.toMatchObject({ status: unavailable.status });

    expect(unavailable.www_authenticate).toBe('');
    expect(getSessionExpired()).toBe(false);
  });

  it('the two rejections answer different statuses, so an outage never mass-expires sessions', () => {
    expect(rejection('auth_verifier_unavailable').status).not.toBe(
      rejection('auth_token_rejected').status,
    );
  });
});
