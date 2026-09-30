export const PASSWORD_REQUIREMENTS_HINT =
  'Use 8+ characters with upper- and lowercase letters, a number, and a symbol.';

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function isValidEmail(email: string): boolean {
  return EMAIL_RE.test(email.trim());
}

type PasswordIssue = 'too_short' | 'no_lowercase' | 'no_uppercase' | 'no_number' | 'no_symbol';

const PASSWORD_RULES: readonly (readonly [PasswordIssue, (password: string) => boolean])[] = [
  ['too_short', (p) => p.length < 8],
  ['no_lowercase', (p) => !/[a-z]/.test(p)],
  ['no_uppercase', (p) => !/[A-Z]/.test(p)],
  ['no_number', (p) => !/[0-9]/.test(p)],
  ['no_symbol', (p) => !/[^A-Za-z0-9]/.test(p)],
];

function validatePassword(password: string): PasswordIssue[] {
  return PASSWORD_RULES.filter(([, failed]) => failed(password)).map(([issue]) => issue);
}

function passwordsMatch(password: string, confirm: string): boolean {
  return confirm.length > 0 && password === confirm;
}

type NewPasswordFormState = {
  showPasswordError: boolean;
  showConfirmError: boolean;
  valid: boolean;
};

export function newPasswordFormState(password: string, confirm: string): NewPasswordFormState {
  const issues = validatePassword(password);
  const matches = passwordsMatch(password, confirm);
  return {
    showPasswordError: issues.length > 0 && password.length > 0,
    showConfirmError: confirm.length > 0 && !matches,
    valid: issues.length === 0 && matches,
  };
}
