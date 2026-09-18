import type { ImperativeRouter } from 'expo-router';
import type { SupabaseClient } from '@supabase/supabase-js';

import {
  type AuthLinkIntent,
  type AuthLinkParams,
  RESET_PASSWORD_ROUTE_SEGMENT,
} from './parseAuthLink';
import { markRecoveryUnlocked } from './recoveryUnlock';

// The slice of the Supabase auth client a link exchange drives; a stub needs
// only these three methods.
type AuthClient = Pick<
  SupabaseClient['auth'],
  'exchangeCodeForSession' | 'setSession' | 'verifyOtp'
>;

// A single OAuth redirect (`altune://auth/callback`) is delivered to two
// independent listeners — useOAuth's in-app browser result and the global
// Linking listener — which both call here and race to consume the same
// single-use credential. The loser fails server-side because the code is
// already spent. We track the last credential we started consuming and skip a
// repeat, so the redirect is processed exactly once regardless of which
// listener wins (see #659).
let lastConsumedCredential: string | null = null;

// The outcome of consuming a link, so callers can report success or failure
// truthfully instead of assuming the exchange worked:
//   - `success`  — verifyOtp/setSession/exchangeCodeForSession resolved cleanly;
//   - `failure`  — the SDK resolved with `{ error }`, or the link lacked the
//                  params needed to complete the intent;
//   - `deduped`  — a concurrent/earlier delivery already claimed this credential;
//   - `ignored`  — the link was not an auth link.
export type AuthIntentResult =
  | { kind: 'success' }
  | { kind: 'failure' }
  | { kind: 'deduped' }
  | { kind: 'ignored' };

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

// Which OTP types each link path may spend. Both halves of a link are written
// by whoever composes it, so `type` alone must not decide what a success
// proves: a genuine signup or email-change token (attacker-triggerable for any
// address) verified under a path that reads `auth/recovery` would otherwise
// unlock the reset form with no recovery credential behind it (#1636). Recovery
// accepts nothing but a recovery token; confirm accepts the two spellings
// Supabase's signup-confirmation templates emit — the only email OTP this app
// ever requests (useSignUp) — and so cannot spend a recovery token either.
const OTP_TYPES_BY_LINK_KIND = {
  recovery: ['recovery'],
  confirm: ['signup', 'email'],
} as const;

type OtpLinkKind = keyof typeof OTP_TYPES_BY_LINK_KIND;
type SpendableOtpType = (typeof OTP_TYPES_BY_LINK_KIND)[OtpLinkKind][number];

// Matched letter-for-letter: a near-miss spelling is a type Supabase would
// reject anyway, so folding case here could only widen what we accept.
function spendableOtpType(
  kind: OtpLinkKind,
  claimed: string | undefined,
): SpendableOtpType | undefined {
  return OTP_TYPES_BY_LINK_KIND[kind].find((spendable) => spendable === claimed);
}

// A token_hash this path may not spend fails closed rather than falling through
// to the token-pair branch, which would hand the same forged link an unlock by
// another route.
async function verifyRecoveryOrConfirm(
  kind: OtpLinkKind,
  params: AuthLinkParams,
  auth: AuthClient,
): Promise<AuthIntentResult> {
  if (!params.token_hash) {
    return setSessionFrom(params, auth);
  }
  const type = spendableOtpType(kind, params.type);
  if (!type) {
    return { kind: 'failure' };
  }
  const { error } = await auth.verifyOtp({ type, token_hash: params.token_hash });
  return error ? { kind: 'failure' } : { kind: 'success' };
}

// Consume an OAuth callback under the PKCE flow: the redirect carries only a
// single-use `code`, which we exchange for a session. A callback with no code —
// e.g. a captured implicit-grant redirect with an inline access/refresh token
// pair — is refused outright; we never hand bare deep-link tokens to setSession,
// since a verified token pair intercepted off the bare `altune` scheme could
// otherwise be replayed (see #655).
async function exchangeOAuth(params: AuthLinkParams, auth: AuthClient): Promise<AuthIntentResult> {
  if (!params.code) {
    return { kind: 'failure' };
  }
  const { error } = await auth.exchangeCodeForSession(params.code);
  return error ? { kind: 'failure' } : { kind: 'success' };
}

// A link that carries a token pair sets the session directly; a link missing
// the params needed to complete its intent is a failure, never a silent no-op.
async function setSessionFrom(params: AuthLinkParams, auth: AuthClient): Promise<AuthIntentResult> {
  if (params.access_token && params.refresh_token) {
    const { error } = await auth.setSession({
      access_token: params.access_token,
      refresh_token: params.refresh_token,
    });
    return error ? { kind: 'failure' } : { kind: 'success' };
  }
  return { kind: 'failure' };
}

export async function completeAuthIntent(
  intent: AuthLinkIntent,
  router: Pick<ImperativeRouter, 'replace'>,
  auth: AuthClient,
): Promise<AuthIntentResult> {
  if (intent.kind === 'ignored') {
    return { kind: 'ignored' };
  }
  const { params } = intent;

  const credential = credentialKey(params);
  if (credential !== null) {
    if (credential === lastConsumedCredential) {
      return { kind: 'deduped' };
    }
    // Claim synchronously, before the first await, so a concurrent second
    // delivery sees the claim and bails instead of racing the exchange.
    lastConsumedCredential = credential;
  }

  if (intent.kind === 'recovery' || intent.kind === 'confirm') {
    const result = await verifyRecoveryOrConfirm(intent.kind, params, auth);
    // Only surface the recovery screen once the link is confirmed good, so a
    // failed verification cannot strand the user on a dead reset form. Unlocking
    // here — and only here — is what lets AuthGate render the password form; a
    // bare deep link never reaches this point (see #656).
    if (result.kind === 'success' && intent.kind === 'recovery') {
      markRecoveryUnlocked();
      router.replace(`/${RESET_PASSWORD_ROUTE_SEGMENT}`);
    }
    return result;
  }

  return exchangeOAuth(params, auth);
}

// The claim outlives a single `it()` — jest runs a file's tests against one
// module instance — so a suite clears it here rather than relying on every test
// inventing an unused credential.
export function _resetConsumedCredentialForTest(): void {
  lastConsumedCredential = null;
}
