import {
  _resetConsumedCredentialForTest,
  _resetSignInHealthForTest,
  completeAuthIntent,
  flushSignInHealth,
  parseAuthLink,
  useSignIn,
} from '@features/signIn';
import {
  createAuthPortMock,
  createSupabaseAuthMock,
  runAsyncAuthHook,
} from '@features/signIn/__tests__/testUtils/authTestUtils';
import { signInTestUser } from '@testing/signInTestUser';
import { flushPromises } from '@testing/flushPromises';

const { signInWithPassword } = createSupabaseAuthMock('signInWithPassword');
const { __http } = require('@testing/doubles/fetch.js');

const EVENTS = 'POST /v1/discovery/events';
const HTTP_ACCEPTED = 202;
const authPort = createAuthPortMock();

type SentRequest = { method: string; path: string; body?: string };

function sentHealth(): Record<string, unknown>[] {
  return (__http.requests as SentRequest[])
    .filter((r) => r.method === 'POST' && r.path === '/v1/discovery/events')
    .map((r) => JSON.parse(r.body ?? '{}') as { type?: string; payload: Record<string, unknown> })
    .filter((event) => event.type === 'playback_health')
    .map((event) => event.payload);
}

beforeEach(async () => {
  await signInTestUser();
  __http.reply(EVENTS, { status: HTTP_ACCEPTED });
  _resetSignInHealthForTest();
  _resetConsumedCredentialForTest();
});

function signIn() {
  return runAsyncAuthHook(useSignIn, (hook) => hook.signIn('a@b.co', 'pw'), authPort);
}

describe('signin_ok: the sign-in SLI numerator', () => {
  it('counts a password sign-in that succeeds', async () => {
    signInWithPassword.mockResolvedValue({ data: { user: {}, session: {} }, error: null });

    await signIn();
    flushSignInHealth();
    await flushPromises();

    expect(sentHealth()).toEqual([expect.objectContaining({ signin_ok: 1 })]);
  });

  it('counts nothing for a sign-in that fails, since that is reported as a failure instead', async () => {
    signInWithPassword.mockResolvedValue({
      data: { user: null, session: null },
      error: { name: 'AuthApiError', status: 400, code: 'invalid_credentials', message: 'no' },
    });

    await signIn();
    flushSignInHealth();
    await flushPromises();

    expect(sentHealth()).toEqual([]);
    expect(authPort.reportSignInFailure).toHaveBeenCalledWith('invalid_credentials');
  });
});

describe('signin_ok through the OAuth exchange', () => {
  const router = { replace: jest.fn() };
  const OAUTH_LINK = 'altune://auth/callback?code=one-time-code';

  function authWith(exchange: jest.Mock) {
    return {
      exchangeCodeForSession: exchange,
      setSession: jest.fn(),
      verifyOtp: jest.fn(),
      getSession: jest.fn(),
    };
  }

  async function healthAfter(run: () => Promise<unknown>): Promise<Record<string, unknown>[]> {
    await run();
    flushSignInHealth();
    await flushPromises();
    return sentHealth();
  }

  it('counts an exchange that succeeds', async () => {
    const auth = authWith(jest.fn().mockResolvedValue({ data: {}, error: null }));

    const health = await healthAfter(() =>
      completeAuthIntent(parseAuthLink(OAUTH_LINK), router, auth),
    );

    expect(health).toEqual([expect.objectContaining({ signin_ok: 1 })]);
  });

  it('counts nothing for an exchange the server refuses', async () => {
    const auth = authWith(
      jest.fn().mockResolvedValue({ data: {}, error: { name: 'AuthApiError', status: 400 } }),
    );

    const health = await healthAfter(() =>
      completeAuthIntent(parseAuthLink(OAUTH_LINK), router, auth),
    );

    expect(health).toEqual([]);
  });

  it('counts a link delivered twice once', async () => {
    const auth = authWith(jest.fn().mockResolvedValue({ data: {}, error: null }));

    const health = await healthAfter(() =>
      Promise.all([
        completeAuthIntent(parseAuthLink(OAUTH_LINK), router, auth),
        completeAuthIntent(parseAuthLink(OAUTH_LINK), router, auth),
      ]),
    );

    expect(health).toEqual([expect.objectContaining({ signin_ok: 1 })]);
  });

  it('counts nothing for a recovery link, which is not a sign-in', async () => {
    const auth = authWith(jest.fn());
    auth.verifyOtp.mockResolvedValue({ data: { user: { id: 'u' }, session: {} }, error: null });

    const health = await healthAfter(() =>
      completeAuthIntent(
        parseAuthLink('altune://auth/recovery?token_hash=h&type=recovery'),
        router,
        auth,
      ),
    );

    expect(health).toEqual([]);
  });
});
