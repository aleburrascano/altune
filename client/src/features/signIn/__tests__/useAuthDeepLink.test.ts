import { startAuthDeepLinks, type LinkEvents } from '@features/signIn';
import { createMemoryNavigator } from '@testing/fakes/navigation/inMemoryNavigator';
import { recordLogs, type RecordedLogs } from '@testing/fakes/log/inMemoryLogger';
import { recordDialogs, type RecordedDialogs } from '@testing/recordDialogs';
import { flushPromises } from '@testing/flushPromises';

type AuthStub = Parameters<typeof startAuthDeepLinks>[0]['auth'];
type UrlListener = (url: string) => void;
type AuthSetup = { exchangeCodeForSession: jest.Mock; verifyOtp: jest.Mock; getSession: jest.Mock };

const RECOVERING_USER_ID = 'user-recovering';
const EXPIRED_OTP = {
  name: 'AuthApiError',
  code: 'otp_expired',
  status: 403,
  message: 'Email link is invalid or has expired',
};

let logs: RecordedLogs;
let dialogs: RecordedDialogs;
let auth: AuthSetup;
let linkCounter = 0;

beforeEach(() => {
  logs = recordLogs();
  dialogs = recordDialogs();
  auth = {
    exchangeCodeForSession: jest.fn().mockResolvedValue({ data: {}, error: null }),
    verifyOtp: jest.fn().mockResolvedValue({
      data: { user: { id: RECOVERING_USER_ID }, session: null },
      error: null,
    }),
    getSession: jest.fn().mockResolvedValue({ data: { session: null }, error: null }),
  };
});

afterEach(() => {
  dialogs.restore();
});

function recoveryLink(): string {
  linkCounter += 1;
  return `altune://auth/recovery?token_hash=super-secret-hash-${linkCounter}&type=recovery`;
}

function verificationRefusedWith(error: unknown): void {
  auth.verifyOtp.mockResolvedValue({ data: { user: null, session: null }, error });
}

function flushMacrotask(): Promise<void> {
  return flushPromises();
}

type LinkSource = { links: LinkEvents; emit(url: string): void; subscribers(): number };

function linksStarting(initial: string | Error | null): LinkSource {
  const listeners = new Set<UrlListener>();
  const links: LinkEvents = {
    initialUrl: () =>
      initial instanceof Error ? Promise.reject(initial) : Promise.resolve(initial),
    subscribeUrl: (listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
  return {
    links,
    emit: (url) => listeners.forEach((listener) => listener(url)),
    subscribers: () => listeners.size,
  };
}

type Started = { source: LinkSource; navigator: ReturnType<typeof createMemoryNavigator> };

async function startWith(initial: string | Error | null): Promise<Started> {
  const source = linksStarting(initial);
  const navigator = createMemoryNavigator();
  startAuthDeepLinks({
    links: source.links,
    router: navigator,
    auth: auth as AuthStub,
    log: { warn },
  });
  await flushMacrotask();
  await flushMacrotask();
  return { source, navigator };
}

function warn(message: string, ...details: unknown[]): void {
  logs.warnings.push([message, ...details]);
}

describe('a rejected completeAuthIntent (#657)', () => {
  it('swallows a rejected link exchange rather than leaking an unhandled rejection', async () => {
    const unhandled = jest.fn();
    process.on('unhandledRejection', unhandled);
    auth.verifyOtp.mockRejectedValue(new Error('transport blew up'));

    await startWith(recoveryLink());

    expect(auth.verifyOtp).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'recovery', token_hash: expect.any(String) }),
    );
    expect(unhandled).not.toHaveBeenCalled();
    process.off('unhandledRejection', unhandled);
  });

  it('spends the code a delivered auth link carries', async () => {
    await startWith('altune://auth/callback?code=abc');

    expect(auth.exchangeCodeForSession).toHaveBeenCalledWith('abc');
  });
});

describe('the trace a link that died in the background leaves (#1647)', () => {
  it('logs the intent kind and the cause when the exchange reports a failure', async () => {
    verificationRefusedWith(EXPIRED_OTP);

    await startWith(recoveryLink());

    expect(logs.warnings).toContainEqual([
      '[auth] deep link exchange failed',
      {
        intent: 'recovery',
        cause: 'gotrue_rejected',
        error: {
          name: 'AuthApiError',
          code: 'otp_expired',
          status: 403,
          message: 'Email link is invalid or has expired',
        },
      },
    ]);
  });

  it('logs the intent kind and the thrown error when the exchange rejects', async () => {
    auth.verifyOtp.mockRejectedValue(new Error('transport blew up'));

    await startWith(recoveryLink());

    expect(logs.warnings).toContainEqual([
      '[auth] deep link exchange threw',
      { intent: 'recovery', name: 'Error', message: 'transport blew up' },
    ]);
  });

  it('logs nothing for a link that completed', async () => {
    const { navigator } = await startWith(recoveryLink());

    expect(navigator.current()).toBe('/reset-password');
    expect(logs.warnings).toEqual([]);
  });

  it('never writes the credential the link carried into the log', async () => {
    verificationRefusedWith(EXPIRED_OTP);

    await startWith(recoveryLink());

    const authLogs = logs.warnings.filter(([message]) => message.startsWith('[auth]'));
    expect(authLogs).toHaveLength(1);
    expect(logs.text()).not.toContain('super-secret-hash');
  });
});

describe('native completes a delivered url event (#2924 probe)', () => {
  it('completes an altune:// auth link delivered as a url event, once', async () => {
    const { source } = await startWith(null);

    source.emit('altune://auth/callback?code=native-event');
    await flushMacrotask();

    expect(auth.exchangeCodeForSession).toHaveBeenCalledTimes(1);
    expect(auth.exchangeCodeForSession).toHaveBeenCalledWith('native-event');
  });

  it('stops listening once the hook is torn down', async () => {
    const source = linksStarting(null);
    const stop = startAuthDeepLinks({
      links: source.links,
      router: createMemoryNavigator(),
      auth: auth as AuthStub,
      log: { warn },
    });

    stop();
    source.emit('altune://auth/callback?code=late');
    await flushMacrotask();

    expect(source.subscribers()).toBe(0);
    expect(auth.exchangeCodeForSession).not.toHaveBeenCalled();
  });
});

describe('a failed link is shown to the user (#686)', () => {
  async function deliverUrlEvent(url: string): Promise<void> {
    const { source } = await startWith(null);
    source.emit(url);
    await flushMacrotask();
  }

  it('alerts that an expired link did not work', async () => {
    verificationRefusedWith(EXPIRED_OTP);

    await deliverUrlEvent(recoveryLink());

    expect(dialogs.alerts).toEqual([["This link didn't work", expect.stringContaining('expired')]]);
  });

  it('alerts to try again when the exchange throws', async () => {
    auth.verifyOtp.mockRejectedValue(new Error('timeout'));

    await deliverUrlEvent(recoveryLink());

    expect(dialogs.alerts).toEqual([["Couldn't open that link", expect.stringContaining('again')]]);
  });

  it('does not alert for a completed link', async () => {
    await deliverUrlEvent(recoveryLink());

    expect(dialogs.alerts).toEqual([]);
  });

  it('catches a rejected initial url read without an unhandled rejection', async () => {
    const unhandled = jest.fn();
    process.on('unhandledRejection', unhandled);

    await startWith(new Error('linking unavailable'));

    expect(logs.warnings).toContainEqual([
      '[auth] initial url read failed',
      { name: 'Error', message: 'linking unavailable' },
    ]);
    expect(unhandled).not.toHaveBeenCalled();
    process.off('unhandledRejection', unhandled);
  });
});
