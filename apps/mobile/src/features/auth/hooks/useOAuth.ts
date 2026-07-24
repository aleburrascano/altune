import { useRouter } from 'expo-router';
import * as WebBrowser from 'expo-web-browser';
import { useState } from 'react';

import { supabase } from '@shared/auth/supabaseClient';
import { completeAuthIntent } from '../lib/completeAuthIntent';
import { parseAuthLink } from '../lib/parseAuthLink';

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
      setState({ kind: 'cancelled' });
    } catch {
      setState({ kind: 'error' });
    }
  }

  return { state, signInWith };
}
