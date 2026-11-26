import { createMemoryAuthPort } from '@features/signIn/__tests__/memoryAuthPort';
import { httpAuthPort, type AuthPort } from '@features/signIn';

const { __http, fakeFetch } = require('@testing/doubles/fetch.js');

const AUTH_FAILURES_PATH = '/v1/public/auth-failures';

type Subject = { port: AuthPort; reported: () => readonly string[] };

const realFetch = global.fetch;

afterEach(() => {
  global.fetch = realFetch;
});

function authPortContract(make: () => Subject): void {
  it('takes each sign-in failure report without throwing, and passes on its reason in order', () => {
    const { port, reported } = make();

    expect(port.reportSignInFailure('invalid_credentials')).toBeUndefined();
    port.reportSignInFailure('network');

    expect(reported()).toEqual(['invalid_credentials', 'network']);
  });
}

describe('in-memory fake', () =>
  authPortContract(() => {
    const port = createMemoryAuthPort();
    return { port, reported: () => port.reportedFailures };
  }));

describe('http adapter', () =>
  authPortContract(() => {
    const posted: string[] = [];
    __http.reply(`POST ${AUTH_FAILURES_PATH}`, { status: 204 });
    global.fetch = ((url: string, init?: RequestInit) => {
      if (String(url).endsWith(AUTH_FAILURES_PATH)) {
        posted.push((JSON.parse(String(init?.body)) as { reason: string }).reason);
      }
      return fakeFetch(url, init);
    }) as typeof fetch;
    return { port: httpAuthPort, reported: () => posted };
  }));
