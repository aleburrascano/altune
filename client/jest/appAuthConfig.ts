import type { AuthClientConfig } from '@/mobile';

export function appAuthConfig(): AuthClientConfig {
  return {
    url: process.env.EXPO_PUBLIC_SUPABASE_URL,
    anonKey: process.env.EXPO_PUBLIC_SUPABASE_ANON_KEY,
  };
}
