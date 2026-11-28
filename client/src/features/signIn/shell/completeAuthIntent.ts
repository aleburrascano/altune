import { type authIntentClient, withAuthDeadline } from '@shared/auth';
import {
  type SupabaseAuthErrorLike,
  supabaseErrorDetail,
  thrownErrorDetail,
} from '@features/signIn/core/errors';
import {
  type AuthFailureCause,
  type AuthIntentFailure,
  type AuthIntentResult,
  RESET_PASSWORD_ROUTE_SEGMENT,
} from '@features/signIn/core/authIntent';
import type { AuthLinkIntent, AuthLinkParams } from '@features/signIn/core/parseAuthLink';
import type { AuthIntents, AuthRouter } from '@features/signIn/ports/ports';
import type { SignInHealth } from '@features/signIn/core/signInHealth';
import type { RecoveryUnlockStore } from '@features/signIn/core/recoveryUnlockStore';
import {
  createConsumedCredentialStore,
  type ConsumedCredentialStore,
  type CredentialConsumption,
} from '@features/signIn/core/consumedCredentialStore';
import { isPresent } from '@features/signIn/core/isPresent';

type AuthClient = ReturnType<typeof authIntentClient>;

type Recovery = Pick<RecoveryUnlockStore, 'mark'>;

type Services = { recovery: Recovery; health: Pick<SignInHealth, 'recordSignInOk'> };

type AuthIntentDeps = Services & { authClient: () => AuthClient };

function refused(cause: Exclude<AuthFailureCause, 'gotrue_rejected'>): AuthIntentFailure {
  return { kind: 'failure', cause };
}

function rejectedByGoTrue(error: SupabaseAuthErrorLike): AuthIntentFailure {
  return { kind: 'failure', cause: 'gotrue_rejected', error: supabaseErrorDetail(error) };
}

function credentialKey(params: AuthLinkParams): string | null {
  if (isPresent(params.code)) {
    return `code:${params.code}`;
  }
  if (isPresent(params.token_hash)) {
    return `token_hash:${params.token_hash}`;
  }
  return null;
}

const OTP_TYPES_BY_LINK_KIND = {
  recovery: ['recovery'],
  confirm: ['signup', 'email'],
} as const;

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
  if (!isPresent(params.token_hash)) {
    return refused('no_spendable_credential');
  }
  if (!type) {
    return refused('otp_type_not_allowed_for_path');
  }
  const { data, error } = await auth.verifyOtp({ type, token_hash: params.token_hash });
  return error ? rejectedByGoTrue(error) : { kind: 'success', userId: data.user?.id ?? null };
}

function openResetPasswordScreenFor(
  userId: string | null,
  router: AuthRouter,
  recovery: Recovery,
): AuthIntentResult {
  if (!isPresent(userId)) {
    return refused('verification_named_no_user');
  }
  recovery.mark(userId);
  router.replace(`/${RESET_PASSWORD_ROUTE_SEGMENT}`);
  return { kind: 'success' };
}

async function exchangeOAuth(params: AuthLinkParams, auth: AuthClient): Promise<AuthIntentResult> {
  if (!isPresent(params.code)) {
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
  recovery: Recovery,
): Promise<AuthIntentResult> {
  const signedIn = await refusalWhileSignedIn(auth);
  if (signedIn) {
    return signedIn;
  }
  const verified = await verifyRecoveryOrConfirm('recovery', params, auth);
  return verified.kind === 'failure'
    ? verified
    : openResetPasswordScreenFor(verified.userId, router, recovery);
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

function countSignIn(outcome: AuthIntentResult, health: Services['health']): AuthIntentResult {
  if (outcome.kind === 'success') health.recordSignInOk();
  return outcome;
}

async function spendCredential(
  intent: Exclude<AuthLinkIntent, { kind: 'ignored' }>,
  router: AuthRouter,
  auth: AuthClient,
  services: Services,
): Promise<AuthIntentResult> {
  switch (intent.kind) {
    case 'recovery':
      return completeRecovery(intent.params, router, auth, services.recovery);
    case 'confirm':
      return confirmSignUp(intent.params, auth);
    case 'oauth':
      return countSignIn(await exchangeOAuth(intent.params, auth), services.health);
    default:
      return unhandledIntent(intent);
  }
}

function claimConsumption(
  consumed: ConsumedCredentialStore,
  credential: string,
  outcome: Promise<AuthIntentResult>,
): Promise<AuthIntentResult> {
  const consumption = { credential, outcome };
  consumed.claim(consumption);
  void releaseUnlessSessionEstablished(consumed, consumption);
  return withAuthDeadline(outcome);
}

async function releaseUnlessSessionEstablished(
  consumed: ConsumedCredentialStore,
  consumption: CredentialConsumption,
): Promise<void> {
  if (await establishedSession(consumption.outcome)) {
    return;
  }
  consumed.releaseIfActive(consumption);
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

async function completeAuthIntent(
  intent: AuthLinkIntent,
  router: AuthRouter,
  auth: AuthClient,
  services: Services,
  consumed: ConsumedCredentialStore,
): Promise<AuthIntentResult> {
  if (intent.kind === 'ignored') {
    return { kind: 'ignored' };
  }
  const credential = credentialKey(intent.params);
  if (credential === null) {
    return spendCredential(intent, router, auth, services);
  }
  const active = consumed.active();
  if (active?.credential === credential) {
    return withAuthDeadline(outcomeOfWinningDelivery(active));
  }
  return claimConsumption(consumed, credential, spendCredential(intent, router, auth, services));
}

export function createAuthIntents({ recovery, health, authClient }: AuthIntentDeps): AuthIntents {
  const consumed = createConsumedCredentialStore();
  return {
    complete: (intent, router) =>
      completeAuthIntent(intent, router, authClient(), { recovery, health }, consumed),
  };
}
