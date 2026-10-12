import * as SecureStore from 'expo-secure-store';

const SESSION_SECONDS = 3600;
const MS_PER_SECOND = 1000;

export async function signInTestUser(): Promise<void> {
  const session = {
    access_token: 'tok',
    refresh_token: 'refresh',
    token_type: 'bearer',
    expires_in: SESSION_SECONDS,
    expires_at: Math.floor(Date.now() / MS_PER_SECOND) + SESSION_SECONDS,
    user: { id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', aud: 'authenticated' },
  };
  await SecureStore.setItemAsync('sb-fixture-auth-token', JSON.stringify(session));
}
