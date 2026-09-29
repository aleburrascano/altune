import { Redirect } from 'expo-router';
import { type ReactElement } from 'react';

export function AuthCallbackScreen(): ReactElement {
  return <Redirect href="/" />;
}
