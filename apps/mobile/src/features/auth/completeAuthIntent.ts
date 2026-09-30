import type { SupabaseClient } from '@supabase/supabase-js';
import type { Navigator } from '@shared/navigation';

import { withAuthDeadline } from './authDeadline';
import { type SupabaseErrorDetail, supabaseErrorDetail, thrownErrorDetail } from './errorDetail';
import type { AuthLinkIntent, AuthLinkParams } from './parseAuthLink';
import { markRecoveryUnlocked } from './recoveryUnlock';
import { RESET_PASSWORD_ROUTE_SEGMENT } from './resetPasswordRoute';
import type { SupabaseAuthErrorLike } from './supabaseAuthError';

type AuthClient = Pick<
  SupabaseClient['auth'],
  'exchangeCodeForSession' | 'verifyOtp' | 'getSession'
>;

type AuthFailureCause =
  | 'gotrue_rejected'
  | 'no_spendable_credential'
  | 'session_already_active'
  | 'otp_type_not_allowed_for_path'
  | 'verification_named_no_user'
  | 'unhandled_intent_kind';

type AuthIntentFailure = {
  kind: 'failure';
  cause: AuthFailureCause;
  error?: SupabaseErrorDetail;
};

export type AuthIntentResult =
  { kind: 'success' } | AuthIntentFailure | { kind: 'deduped' } | { kind: 'ignored' };

function refused(cause: Exclude<AuthFailureCause, 'gotrue_rejected'>): AuthIntentFailure {
  return { kind: 'failure', cause };
}

function rejectedByGoTrue(error: SupabaseAuthErrorLike): AuthIntentFailure {
  return { kind: 'failure', cause: 'gotrue_rejected', error: supabaseErrorDetail(error) };
}

type CredentialConsumption = { credential: string; outcome: Promise<AuthIntentResult> };

let activeConsumption: CredentialConsumption | null = null;

function credentialKey(params: AuthLinkParams): string | null {
  if (params.code) {
    return `code:${params.code}`;
  }
  if (params.token_hash) {
    return `token_hash:${params.token_hash}`;
  }
  return null;
}

const OTP_TYPES_BY_LINK_KIND = {
  recovery: ['recovery'],
  confirm: ['signup', 'email'],
} as const;

export type AuthRouter = Pick<Navigator, 'replace'>;

type OtpLinkKind = keyof typeof OTP_TYPES_BY_LINK_KIND;
type SpendableOtpType = (typeof OTP_TYPES_BY_LINK_KIND)[OtpLinkKind][number];

function spendableOtpType(
  kind: OtpLinkKind,
  claimed: string | undefined,
): SpendableOtpType | undefined {
  return OTP_TYPES_BY_LINK_KIND[kind].find((spendable) => spendable === claimed);
}

type VerifiedOtp = { kind: 'success'; userId: string | null } | AuthIntentFailure;

async function verifyRecoveryOrConfirm(
  kind: OtpLinkKind,
  params: AuthLinkParams,
  auth: AuthClient,
): Promise<VerifiedOtp> {
  const type = spendableOtpType(kind, params.type);
  if (!params.token_hash) {
    return refused('no_spendable_credential');
  }
  if (!type) {
    return refused('otp_type_not_allowed_for_path');
  }
  const { data, error } = await auth.verifyOtp({ type, token_hash: params.token_hash });
  return error ? rejectedByGoTrue(error) : { kind: 'success', userId: data.user?.id ?? null };
}

function openResetPasswordScreenFor(userId: string | null, router: AuthRouter): AuthIntentResult {
  if (!userId) {
    return refused('verification_named_no_user');
  }
  markRecoveryUnlocked(userId);
  router.replace(`/${RESET_PASSWORD_ROUTE_SEGMENT}`);
  return { kind: 'success' };
}

async function exchangeOAuth(params: AuthLinkParams, auth: AuthClient): Promise<AuthIntentResult> {
  if (!params.code) {
    return refused('no_spendable_credential');
  }
  const { error } = await auth.exchangeCodeForSession(params.code);
  return error ? rejectedByGoTrue(error) : { kind: 'success' };
}

async function sessionRefusal(auth: AuthClient): Promise<AuthIntentFailure | null> {
  const { data: sessionResult, error } = await auth.getSession();
  if (error) {
    return rejectedByGoTrue(error);
  }
  return sessionResult.session ? refused('session_already_active') : null;
}

async function refusalWhileSignedIn(auth: AuthClient): Promise<AuthIntentFailure | null> {
  try {
    return await sessionRefusal(auth);
  } catch (thrown) {
    return { kind: 'failure', cause: 'gotrue_rejected', error: thrownErrorDetail(thrown) };
  }
}

async function completeRecovery(
  params: AuthLinkParams,
  router: AuthRouter,
  auth: AuthClient,
): Promise<AuthIntentResult> {
  const signedIn = await refusalWhileSignedIn(auth);
  if (signedIn) {
    return signedIn;
  }
  const verified = await verifyRecoveryOrConfirm('recovery', params, auth);
  return verified.kind === 'failure'
    ? verified
    : openResetPasswordScreenFor(verified.userId, router);
}

async function confirmSignUp(params: AuthLinkParams, auth: AuthClient): Promise<AuthIntentResult> {
  const signedIn = await refusalWhileSignedIn(auth);
  if (signedIn) {
    return signedIn;
  }
  const verified = await verifyRecoveryOrConfirm('confirm', params, auth);
  return verified.kind === 'failure' ? verified : { kind: 'success' };
}

function unhandledIntent(_intent: never): AuthIntentResult {
  return refused('unhandled_intent_kind');
}

async function spendCredential(
  intent: Exclude<AuthLinkIntent, { kind: 'ignored' }>,
  router: AuthRouter,
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

function claimConsumption(
  credential: string,
  outcome: Promise<AuthIntentResult>,
): Promise<AuthIntentResult> {
  const consumption = { credential, outcome };
  activeConsumption = consumption;
  void releaseUnlessSessionEstablished(consumption);
  return withAuthDeadline(outcome);
}

async function releaseUnlessSessionEstablished(consumption: CredentialConsumption): Promise<void> {
  if (await establishedSession(consumption.outcome)) {
    return;
  }
  if (activeConsumption === consumption) {
    activeConsumption = null;
  }
}

async function establishedSession(outcome: Promise<AuthIntentResult>): Promise<boolean> {
  try {
    return (await outcome).kind === 'success';
  } catch {
    return false;
  }
}

async function outcomeOfWinningDelivery(
  consumption: CredentialConsumption,
): Promise<AuthIntentResult> {
  const outcome = await consumption.outcome;
  return outcome.kind === 'success' ? { kind: 'deduped' } : outcome;
}

export async function completeAuthIntent(
  intent: AuthLinkIntent,
  router: AuthRouter,
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
    return withAuthDeadline(outcomeOfWinningDelivery(activeConsumption));
  }
  return claimConsumption(credential, spendCredential(intent, router, auth));
}

export function _resetConsumedCredentialForTest(): void {
  activeConsumption = null;
}
