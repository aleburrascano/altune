import { useRouter } from 'expo-router';
import * as WebBrowser from 'expo-web-browser';
import { useState } from 'react';

import { supabase } from '@shared/auth/supabaseClient';
import { completeAuthIntent } from '../completeAuthIntent';
import { OAUTH_REDIRECT_URL, parseAuthLink } from '../parseAuthLink';

WebBrowser.maybeCompleteAuthSession();

export type OAuthProvider = 'google';

export type OAuthResult =
  | { kind: 'idle' }
  | { kind: 'pending'; provider: OAuthProvider }
  | { kind: 'ok' }
  | { kind: 'cancelled' }
  | { kind: 'error' };

export function useOAuth() {
  const router = useRouter();
  const [state, setState] = useState<OAuthResult>({ kind: 'idle' });

  async function signInWith(provider: OAuthProvider): Promise<void> {
    setState({ kind: 'pending', provider });
    try {
      const { data, error } = await supabase.auth.signInWithOAuth({
        provider,
        options: { redirectTo: OAUTH_REDIRECT_URL, skipBrowserRedirect: true },
      });
      if (error || !data?.url) {
        setState({ kind: 'error' });
        return;
      }
      const result = await WebBrowser.openAuthSessionAsync(data.url, OAUTH_REDIRECT_URL);
      if (result.type === 'success' && result.url) {
        const outcome = await completeAuthIntent(parseAuthLink(result.url), router, supabase.auth);
        // `ok` only if the code exchange actually succeeded. `deduped` means the
        // global deep-link listener already consumed this callback and
        // established the session, so it is a success too. Anything else — a
        // rejected exchange or an unrecognized callback — is a real error.
        setState(
          outcome.kind === 'success' || outcome.kind === 'deduped'
            ? { kind: 'ok' }
            : { kind: 'error' },
        );
        return;
      }
      setState({ kind: 'cancelled' });
    } catch {
      setState({ kind: 'error' });
    }
  }

  return { state, signInWith };
}
