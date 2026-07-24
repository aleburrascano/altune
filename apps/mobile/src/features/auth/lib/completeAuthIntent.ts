import type { Router } from 'expo-router';

import { supabase } from '@shared/auth/supabaseClient';

import type { AuthLinkIntent } from './parseAuthLink';

type VerifyOtpArg = Parameters<typeof supabase.auth.verifyOtp>[0];

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
      router.replace('/reset-password');
    }
    return;
  }

  if (params.code) {
    await supabase.auth.exchangeCodeForSession(params.code);
  } else if (params.access_token && params.refresh_token) {
    await supabase.auth.setSession({
      access_token: params.access_token,
      refresh_token: params.refresh_token,
    });
  }
}
