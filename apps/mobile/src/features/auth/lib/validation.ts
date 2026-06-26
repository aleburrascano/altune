/**
 * Auth form validation — pure, feature-local helpers.
 *
 * Client-side validation is UX (instant feedback, saved round-trips); the
 * Supabase server-side policy remains the security backstop. The client
 * password minimum here MUST mirror the project's Supabase password policy
 * (see docs/specs/auth-hardening/plan.md prerequisites).
 *
 * No messages live here — copy is a UI concern. These return facts, the
 * screens decide wording (and keep credential wording generic per AC#5).
 */

/** Client password minimum; mirror the Supabase dashboard policy. */
export const DEFAULT_PASSWORD_MIN_LENGTH = 8;

// Pragmatic email shape check (not RFC-perfect): non-empty local part, an @,
// a domain, a dot, and a TLD — with no whitespace and a single @. Server
// validation + the confirmation email are the real proof of deliverability.
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function isValidEmail(email: string): boolean {
  return EMAIL_RE.test(email.trim());
}

export type PasswordIssue = 'too_short';

/** Returns the list of policy issues; an empty array means valid. */
export function validatePassword(
  password: string,
  minLength: number = DEFAULT_PASSWORD_MIN_LENGTH,
): PasswordIssue[] {
  const issues: PasswordIssue[] = [];
  if (password.length < minLength) {
    issues.push('too_short');
  }
  return issues;
}

/** True only when the confirm field is non-empty and equals the password. */
export function passwordsMatch(password: string, confirm: string): boolean {
  return confirm.length > 0 && password === confirm;
}
