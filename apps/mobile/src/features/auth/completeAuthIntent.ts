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

// The outcome of consuming a link, so callers can report success or failure
// truthfully instead of assuming the exchange worked:
//   - `success`  — verifyOtp/exchangeCodeForSession resolved cleanly;
//   - `failure`  — the SDK resolved with `{ error }`, or the link lacked the
//                  params needed to complete the intent;
//   - `deduped`  — another delivery of this credential established the session;
//   - `ignored`  — the link was not an auth link.
export type AuthIntentResult =
  | { kind: 'success' }
  | { kind: 'failure' }
  | { kind: 'deduped' }
  | { kind: 'ignored' };

// A single OAuth redirect (`altune://auth/callback`) is delivered to two
// independent listeners — useOAuth's in-app browser result and the global
// Linking listener — which both call here and race to consume the same
// single-use credential. The loser would fail server-side because the code is
// already spent, so the credential being consumed is held together with the
// exchange that is spending it: the loser awaits that very promise and reports
// what the winner actually got, instead of a success nobody has (#659, #1641).
type CredentialConsumption = { credential: string; outcome: Promise<AuthIntentResult> };

let activeConsumption: CredentialConsumption | null = null;

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

// Only surface the recovery screen once the link is confirmed good, so a failed
// verification cannot strand the user on a dead reset form. Unlocking here — and
// only here — is what lets AuthGate render the password form; a bare deep link
// never reaches this point (see #656).
async function completeRecovery(
  params: AuthLinkParams,
  router: Pick<ImperativeRouter, 'replace'>,
  auth: AuthClient,
): Promise<AuthIntentResult> {
  const verified = await verifyRecoveryOrConfirm('recovery', params, auth);
  return verified.kind === 'failure'
    ? { kind: 'failure' }
    : openResetPasswordScreenFor(verified.userId, router);
}

// A confirmed signup needs no navigation: the session the verification
// established is what AuthGate reads on its next render.
async function confirmSignUp(params: AuthLinkParams, auth: AuthClient): Promise<AuthIntentResult> {
  const verified = await verifyRecoveryOrConfirm('confirm', params, auth);
  return { kind: verified.kind === 'failure' ? 'failure' : 'success' };
}

// A kind with no case above is one added to `AuthLinkIntent` without deciding
// how its link is spent. The `never` parameter makes that a compile error, so a
// new kind can no longer inherit whichever branch happened to be last — which
// was the PKCE exchange, spending the link against a flow it never named
// (#1644). Should one reach here anyway it is refused, not guessed at.
function unhandledIntent(_intent: never): AuthIntentResult {
  return { kind: 'failure' };
}

async function spendCredential(
  intent: Exclude<AuthLinkIntent, { kind: 'ignored' }>,
  router: Pick<ImperativeRouter, 'replace'>,
  auth: AuthClient,
): Promise<AuthIntentResult> {
  switch (intent.kind) {
    case 'recovery':
      return completeRecovery(intent.params, router, auth);
    case 'confirm':
      return confirmSignUp(intent.params, auth);
    case 'oauth':
      return exchangeOAuth(intent.params, auth);
    default:
      return unhandledIntent(intent);
  }
}

// The claim is taken synchronously, before the exchange is awaited, so a second
// delivery arriving mid-exchange finds it rather than racing it.
function claimConsumption(
  credential: string,
  outcome: Promise<AuthIntentResult>,
): Promise<AuthIntentResult> {
  const consumption = { credential, outcome };
  activeConsumption = consumption;
  void releaseUnlessSessionEstablished(consumption);
  return outcome;
}

// Only a confirmed success keeps the claim. The link behind a failed exchange is
// still the live link in the user's inbox — a transient 5xx or a dropped
// connection spends nothing — and a claim kept on it would answer every later
// tap `deduped`, blocking the retry forever (#1641).
async function releaseUnlessSessionEstablished(consumption: CredentialConsumption): Promise<void> {
  if (await establishedSession(consumption.outcome)) {
    return;
  }
  // Never clear a claim a later, different credential has since taken.
  if (activeConsumption === consumption) {
    activeConsumption = null;
  }
}

// A rejected exchange — the SDK throws on a transport failure — established no
// session either, so it must not hold the credential.
async function establishedSession(outcome: Promise<AuthIntentResult>): Promise<boolean> {
  try {
    return (await outcome).kind === 'success';
  } catch {
    return false;
  }
}

// What the delivery that lost the race reports. `deduped` asserts the other
// listener established the session, so only the winner's real success may earn
// it; any other outcome is the winner's own, told straight (#1641).
async function outcomeOfWinningDelivery(
  consumption: CredentialConsumption,
): Promise<AuthIntentResult> {
  const outcome = await consumption.outcome;
  return outcome.kind === 'success' ? { kind: 'deduped' } : outcome;
}

export async function completeAuthIntent(
  intent: AuthLinkIntent,
  router: Pick<ImperativeRouter, 'replace'>,
  auth: AuthClient,
): Promise<AuthIntentResult> {
  if (intent.kind === 'ignored') {
    return { kind: 'ignored' };
  }
  const credential = credentialKey(intent.params);
  if (credential === null) {
    return spendCredential(intent, router, auth);
  }
  if (activeConsumption?.credential === credential) {
    return outcomeOfWinningDelivery(activeConsumption);
  }
  return claimConsumption(credential, spendCredential(intent, router, auth));
}

// The claim outlives a single `it()` — jest runs a file's tests against one
// module instance — so a suite clears it here rather than relying on every test
// inventing an unused credential.
export function _resetConsumedCredentialForTest(): void {
  activeConsumption = null;
}
