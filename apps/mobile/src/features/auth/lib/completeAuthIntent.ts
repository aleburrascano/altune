/**
 * completeAuthIntent — exchanges a classified deep link's tokens for a
 * Supabase session, then routes if the flow requires it.
 *
 * Part of the single deep-link spine: `parseAuthLink` classifies, this acts.
 * Consumed by both useAuthDeepLink (incoming `altune://` links) and useOAuth
 * (the native auth-session's returned callback URL). Confirm/OAuth just
 * establish a session (AuthGate then routes); recovery additionally navigates
 * to the set-new-password screen.
 *
 * AIDEV-NOTE: `parseAuthLink` whitelists by PATH so this is robust to
 * token_hash-vs-access_token; it reads whichever params are present.
 */
import type { Router } from 'expo-router';

import { supabase } from '@shared/auth/supabaseClient';

import type { AuthLinkIntent } from './parseAuthLink';

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
