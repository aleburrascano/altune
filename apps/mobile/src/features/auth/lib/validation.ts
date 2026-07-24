export const DEFAULT_PASSWORD_MIN_LENGTH = 8;

export const PASSWORD_REQUIREMENTS_HINT =
  'Use 8+ characters with upper- and lowercase letters, a number, and a symbol.';

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

export function passwordsMatch(password: string, confirm: string): boolean {
  return confirm.length > 0 && password === confirm;
}
