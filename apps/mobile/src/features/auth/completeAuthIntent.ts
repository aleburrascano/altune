import type { ImperativeRouter } from 'expo-router';
import type { SupabaseClient } from '@supabase/supabase-js';

import {
  type AuthLinkIntent,
  type AuthLinkParams,
  RESET_PASSWORD_ROUTE_SEGMENT,
} from './parseAuthLink';
import { markRecoveryUnlocked } from './recoveryUnlock';

// The slice of the Supabase auth client a link exchange drives; a stub needs
// only these two methods. `setSession` is deliberately absent: every link is
// spent against the server (a PKCE `code`, or a `token_hash` this path may
// verify), so no deep-link param can become a session on its own word. A token
// pair intercepted off the unverified `altune` scheme has nothing here to
// replay against, whichever path carried it (#655, #1637).
type AuthClient = Pick<SupabaseClient['auth'], 'exchangeCodeForSession' | 'verifyOtp'>;

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
//   - `success`  — verifyOtp/exchangeCodeForSession resolved cleanly;
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
// Only a credential this module can actually spend is keyed: claiming a shape
// we refuse would answer its second delivery `deduped` — which callers read as
// "the other listener is establishing the session" — and so launder a refusal
// into a success (#1637).
function credentialKey(params: AuthLinkParams): string | null {
  if (params.code) {
    return `code:${params.code}`;
  }
  if (params.token_hash) {
    return `token_hash:${params.token_hash}`;
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

// A verified exchange, carrying the identity the server attributed the token to
// so a recovery can be bound to it. `userId` is absent when the server verified
// the token without naming a user.
type VerifiedOtp = { kind: 'success'; userId: string | null } | { kind: 'failure' };

// A `token_hash` the server verifies is the only credential these links may
// spend. A link carrying anything else — an implicit-grant token pair, or a
// token_hash of a type this path may not spend — fails closed, because every
// alternative route to a session is one a forged or intercepted link would take
// instead (#1637).
async function verifyRecoveryOrConfirm(
  kind: OtpLinkKind,
  params: AuthLinkParams,
  auth: AuthClient,
): Promise<VerifiedOtp> {
  const type = spendableOtpType(kind, params.type);
  if (!params.token_hash || !type) {
    return { kind: 'failure' };
  }
  const { data, error } = await auth.verifyOtp({ type, token_hash: params.token_hash });
  return error ? { kind: 'failure' } : { kind: 'success', userId: data.user?.id ?? null };
}

// The unlock is bound to the user the server just named on the verification,
// never to whoever is signed in by the time the form renders — that is what
// keeps an abandoned recovery from handing the next account on the device a
// password reset it proved nothing for (#1638). An exchange that names no user
// cannot be bound to one, so it fails closed rather than unlocking the form for
// every identity.
function openResetPasswordScreenFor(
  userId: string | null,
  router: Pick<ImperativeRouter, 'replace'>,
): AuthIntentResult {
  if (!userId) {
    return { kind: 'failure' };
  }
  markRecoveryUnlocked(userId);
  router.replace(`/${RESET_PASSWORD_ROUTE_SEGMENT}`);
  return { kind: 'success' };
}

// Under the PKCE flow the callback carries only a single-use `code`, worthless
// without the verifier we hold. A callback with no code — e.g. a captured
// implicit-grant redirect with an inline access/refresh token pair — is refused
// outright (see #655).
async function exchangeOAuth(params: AuthLinkParams, auth: AuthClient): Promise<AuthIntentResult> {
  if (!params.code) {
    return { kind: 'failure' };
  }
  const { error } = await auth.exchangeCodeForSession(params.code);
  return error ? { kind: 'failure' } : { kind: 'success' };
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
    const verified = await verifyRecoveryOrConfirm(intent.kind, params, auth);
    if (verified.kind === 'failure') {
      return { kind: 'failure' };
    }
    // Only surface the recovery screen once the link is confirmed good, so a
    // failed verification cannot strand the user on a dead reset form. Unlocking
    // here — and only here — is what lets AuthGate render the password form; a
    // bare deep link never reaches this point (see #656).
    return intent.kind === 'recovery'
      ? openResetPasswordScreenFor(verified.userId, router)
      : { kind: 'success' };
  }

  return exchangeOAuth(params, auth);
}

// The claim outlives a single `it()` — jest runs a file's tests against one
// module instance — so a suite clears it here rather than relying on every test
// inventing an unused credential.
export function _resetConsumedCredentialForTest(): void {
  lastConsumedCredential = null;
}
