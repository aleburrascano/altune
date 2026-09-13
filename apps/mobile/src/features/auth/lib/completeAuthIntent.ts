import type { ImperativeRouter } from 'expo-router';

import { supabase } from '@shared/auth/supabaseClient';

import type { AuthLinkIntent, AuthLinkParams } from './parseAuthLink';

// A single OAuth redirect (`altune://auth/callback`) is delivered to two
// independent listeners — useOAuth's in-app browser result and the global
// Linking listener — which both call here and race to consume the same
// single-use credential. The loser fails server-side because the code is
// already spent. We track the last credential we started consuming and skip a
// repeat, so the redirect is processed exactly once regardless of which
// listener wins (see #659).
let lastConsumedCredential: string | null = null;

// The single-use credential carried by the link, if any. Two deliveries of the
// same redirect carry the identical credential, so it is a stable dedupe key.
function credentialKey(params: AuthLinkParams): string | null {
  if (params.code) {
    return `code:${params.code}`;
  }
  if (params.token_hash) {
    return `token_hash:${params.token_hash}`;
  }
  if (params.access_token) {
    return `access_token:${params.access_token}`;
  }
  return null;
}

export async function completeAuthIntent(
  intent: AuthLinkIntent,
  router: Pick<ImperativeRouter, 'replace'>,
): Promise<void> {
  if (intent.kind === 'ignored') {
    return;
  }
  const { params } = intent;

  const credential = credentialKey(params);
  if (credential !== null) {
    if (credential === lastConsumedCredential) {
      return;
    }
    // Claim synchronously, before the first await, so a concurrent second
    // delivery sees the claim and bails instead of racing the exchange.
    lastConsumedCredential = credential;
  }

  if (intent.kind === 'recovery' || intent.kind === 'confirm') {
    if (params.token_hash && params.type) {
      await supabase.auth.verifyOtp({
        type: params.type,
        token_hash: params.token_hash,
      });
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
