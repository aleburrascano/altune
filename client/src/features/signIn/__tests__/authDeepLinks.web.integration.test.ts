import { installAuthClient } from '@shared/auth';
import { createMemoryNavigator } from '@testing/fakes/navigation/inMemoryNavigator';
import { authClientOver } from '@testing/fakeAuthClient';

import { nativeLinkEvents, startAppAuthDeepLinks } from '@features/signIn';

const WEB_CALLBACK_PATH = '/auth/callback?code=web-code';

function flushMacrotask(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

describe('auth deep links on web leave the page URL to the route (#2924)', () => {
  it('spends no credential on the page URL when the app starts its deep links', async () => {
    const exchangeCodeForSession = jest.fn();
    const verifyOtp = jest.fn();
    installAuthClient(authClientOver({ exchangeCodeForSession, verifyOtp }));
    window.history.pushState({}, '', WEB_CALLBACK_PATH);

    const stop = startAppAuthDeepLinks(nativeLinkEvents, createMemoryNavigator());
    await flushMacrotask();
    stop();

    expect(exchangeCodeForSession).not.toHaveBeenCalled();
    expect(verifyOtp).not.toHaveBeenCalled();
  });
});
