/**
 * useAuthDeepLink — the single deep-link spine for auth callbacks.
 *
 * All three return paths (email-confirm, password-recovery, OAuth) arrive as
 * `altune://` links. This subscribes to incoming URLs, classifies them with
 * the pure `parseAuthLink`, and hands the tokens to Supabase. Confirm/OAuth
 * just establish a session (AuthGate then routes); recovery additionally
 * navigates to the set-new-password screen.
 *
 * AIDEV-NOTE: live verification is gated on the Supabase dashboard registering
 * the `altune://auth/{confirm,recovery,callback}` redirect URLs and the email
 * templates' token style. `parseAuthLink` whitelists by PATH so it's robust to
 * token_hash-vs-access_token; `completeAuthIntent` reads whichever is present.
 */
import * as Linking from 'expo-linking';
import { useRouter, type Router } from 'expo-router';
import { useEffect } from 'react';

import { supabase } from '../api/supabaseClient';
import { parseAuthLink, type AuthLinkIntent } from '../lib/parseAuthLink';

type VerifyOtpArg = Parameters<typeof supabase.auth.verifyOtp>[0];

/** Exchange the link's tokens for a Supabase session, then route if needed. */
export async function completeAuthIntent(
  intent: AuthLinkIntent,
  router: Pick<Router, 'replace'>,
): Promise<void> {
  if (intent.kind === 'ignored') {
    return;
  }
  const { params } = intent;

  if (intent.kind === 'recovery' || intent.kind === 'confirm') {
    if (params.token_hash && params.type) {
      await supabase.auth.verifyOtp({
        type: params.type,
        token_hash: params.token_hash,
      } as VerifyOtpArg);
    } else if (params.access_token && params.refresh_token) {
      await supabase.auth.setSession({
        access_token: params.access_token,
        refresh_token: params.refresh_token,
      });
    }
    if (intent.kind === 'recovery') {
      // The recovery session is now active; the set-new-password screen is a
      // top-level route AuthGate lets through despite the session being valid.
      router.replace('/reset-password');
    }
    return;
  }

  // oauth
  if (params.code) {
    await supabase.auth.exchangeCodeForSession(params.code);
  } else if (params.access_token && params.refresh_token) {
    await supabase.auth.setSession({
      access_token: params.access_token,
      refresh_token: params.refresh_token,
    });
  }
}

export function useAuthDeepLink(): void {
  const router = useRouter();

  useEffect(() => {
    let active = true;

    const handle = (url: string | null): void => {
      if (!url || !active) {
        return;
      }
      void completeAuthIntent(parseAuthLink(url), router);
    };

    void Linking.getInitialURL().then(handle);
    const sub = Linking.addEventListener('url', ({ url }) => handle(url));

    return () => {
      active = false;
      sub.remove();
    };
  }, [router]);
}
