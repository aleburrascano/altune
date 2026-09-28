import { useState } from 'react';

import { isValidEmail } from '../validation';

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
