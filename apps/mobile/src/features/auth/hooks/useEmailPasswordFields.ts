import { useState } from 'react';

import { isValidEmail } from '../validation';

// `credentials` carries the trimmed email so both credential screens submit the
// same value the email rule was checked against, whatever the user pasted.
export function useEmailPasswordFields() {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');

  const emailValid = isValidEmail(email);

  return {
    email,
    setEmail,
    password,
    setPassword,
    emailValid,
    showEmailError: email.length > 0 && !emailValid,
    credentials: { email: email.trim(), password },
  };
}
