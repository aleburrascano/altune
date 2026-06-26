/**
 * useOAuth — one-tap sign-in with Apple or Google (ADR-0018).
 *
 * Supabase mints the provider auth URL (`signInWithOAuth`, browser redirect
 * skipped); we open it in a native auth session and feed the returned callback
 * URL back through the deep-link spine (`parseAuthLink` + `completeAuthIntent`)
 * to exchange the code for a session. One code path covers both providers.
 *
 * AIDEV-NOTE: live verification needs the Supabase Apple/Google providers
 * registered + `altune://auth/callback` whitelisted (see plan prerequisites).
 * Native one-tap sheets (expo-apple-authentication) are a deferred enhancement.
 */
import { useRouter } from 'expo-router';
import * as WebBrowser from 'expo-web-browser';
import { useState } from 'react';

import { supabase } from '../api/supabaseClient';
import { completeAuthIntent } from './useAuthDeepLink';
import { parseAuthLink } from '../lib/parseAuthLink';

// Required by expo-web-browser to settle any pending auth session on web.
WebBrowser.maybeCompleteAuthSession();

export const OAUTH_REDIRECT_URL = 'altune://auth/callback';

export type OAuthProvider = 'apple' | 'google';

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
        await completeAuthIntent(parseAuthLink(result.url), router);
        setState({ kind: 'ok' });
        return;
      }
      // User dismissed the sheet or it closed without a callback URL.
      setState({ kind: 'cancelled' });
    } catch {
      setState({ kind: 'error' });
    }
  }

  return { state, signInWith };
}
