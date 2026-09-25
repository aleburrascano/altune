import { newPasswordFormState } from '../validation';

describe('newPasswordFormState', () => {
  const strong = 'Abcdef1!';
  const none = { showPasswordError: false, showConfirmError: false, valid: false };

  it.each([
    ['empty', '', '', none],
    ['short', 'Ab1!', 'Ab1!', { ...none, showPasswordError: true }],
    ['valid, confirm empty', strong, '', none],
    ['mismatch', strong, 'Abcdef1?', { ...none, showConfirmError: true }],
    ['match', strong, strong, { ...none, valid: true }],
  ])('%s', (_name, password, confirm, expected) => {
    expect(newPasswordFormState(password, confirm)).toEqual(expected);
  });
});
