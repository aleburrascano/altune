import type { SupabaseErrorDetail } from '@features/signIn/core/errors';

export type AuthFailureCause =
  | 'gotrue_rejected'
  | 'no_spendable_credential'
  | 'session_already_active'
  | 'otp_type_not_allowed_for_path'
  | 'verification_named_no_user'
  | 'unhandled_intent_kind';

export type AuthIntentFailure = {
  kind: 'failure';
  cause: AuthFailureCause;
  error?: SupabaseErrorDetail;
};

export type AuthIntentResult =
  { kind: 'success' } | AuthIntentFailure | { kind: 'deduped' } | { kind: 'ignored' };

export const RESET_PASSWORD_ROUTE_SEGMENT = 'reset-password';
