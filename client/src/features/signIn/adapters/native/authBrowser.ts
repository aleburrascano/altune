import * as WebBrowser from 'expo-web-browser';

import type { AuthBrowser } from '@features/signIn/ports/authBrowser';

WebBrowser.maybeCompleteAuthSession();

export const nativeAuthBrowser: AuthBrowser = {
  openAuthSession: (url, redirectUrl) => WebBrowser.openAuthSessionAsync(url, redirectUrl),
  dismissAuthSession: () => {
    try {
      WebBrowser.dismissAuthSession();
    } catch {
      return;
    }
  },
};
