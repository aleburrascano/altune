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

// Client policy — MUST mirror the Supabase dashboard password policy
// (Authentication → Policies). Current dashboard setting: min length 8 +
// lowercase + uppercase + digit + symbol. If you change one, change both,
// or users hit a server rejection the client said was fine.
export const DEFAULT_PASSWORD_MIN_LENGTH = 8;

/** Human-facing summary of the policy, shown as inline field guidance. */
export const PASSWORD_REQUIREMENTS_HINT =
  'Use 8+ characters with upper- and lowercase letters, a number, and a symbol.';

// Pragmatic email shape check (not RFC-perfect): non-empty local part, an @,
// a domain, a dot, and a TLD — with no whitespace and a single @. Server
// validation + the confirmation email are the real proof of deliverability.
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function isValidEmail(email: string): boolean {
  return EMAIL_RE.test(email.trim());
}

export type PasswordIssue =
  | 'too_short'
  | 'no_lowercase'
  | 'no_uppercase'
  | 'no_number'
  | 'no_symbol';

/** Returns the list of policy issues; an empty array means valid. */
export function validatePassword(
  password: string,
  minLength: number = DEFAULT_PASSWORD_MIN_LENGTH,
): PasswordIssue[] {
  const issues: PasswordIssue[] = [];
  if (password.length < minLength) {
    issues.push('too_short');
  }
  if (!/[a-z]/.test(password)) {
    issues.push('no_lowercase');
  }
  if (!/[A-Z]/.test(password)) {
    issues.push('no_uppercase');
  }
  if (!/[0-9]/.test(password)) {
    issues.push('no_number');
  }
  if (!/[^A-Za-z0-9]/.test(password)) {
    issues.push('no_symbol');
  }
  return issues;
}

/** True only when the confirm field is non-empty and equals the password. */
export function passwordsMatch(password: string, confirm: string): boolean {
  return confirm.length > 0 && password === confirm;
}
