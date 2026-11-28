import { installAuthClient } from '@shared/auth';
import { createMemoryNavigator } from '@testing/fakes/navigation/inMemoryNavigator';
import { authClientOver } from '@testing/fakeAuthClient';

import { nativeLinkEvents } from '@features/signIn/adapters/native/nativeLinkEvents';
import { startAppAuthDeepLinks } from '@features/signIn/shell/authDeepLinks';
import { intents } from '@features/signIn/__tests__/authTestUtils';
import { flushPromises } from '@testing/flushPromises';

const WEB_CALLBACK_PATH = '/auth/callback?code=web-code';

function flushMacrotask(): Promise<void> {
  return flushPromises();
}

describe('auth deep links on web leave the page URL to the route (#2924)', () => {
  it('spends no credential on the page URL when the app starts its deep links', async () => {
    const exchangeCodeForSession = jest.fn();
    const verifyOtp = jest.fn();
    installAuthClient(authClientOver({ exchangeCodeForSession, verifyOtp }));
    window.history.pushState({}, '', WEB_CALLBACK_PATH);

    const stop = startAppAuthDeepLinks(nativeLinkEvents, createMemoryNavigator(), intents);
    await flushMacrotask();
    stop();

    expect(exchangeCodeForSession).not.toHaveBeenCalled();
    expect(verifyOtp).not.toHaveBeenCalled();
  });

  it('hands no url event to the listener when the page posts a message', async () => {
    const listener = jest.fn();
    const stop = nativeLinkEvents.subscribeUrl(listener);

    window.history.pushState({}, '', WEB_CALLBACK_PATH);
    window.dispatchEvent(new MessageEvent('message'));
    await flushMacrotask();
    stop();

    expect(listener).not.toHaveBeenCalled();
  });
});
