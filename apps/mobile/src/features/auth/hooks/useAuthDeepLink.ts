/**
 * useAuthDeepLink — the single deep-link spine for auth callbacks.
 *
 * All three return paths (email-confirm, password-recovery, OAuth) arrive as
 * `altune://` links. This subscribes to incoming URLs, classifies them with
 * the pure `parseAuthLink`, and hands the tokens to `completeAuthIntent`
 * (lib/), which establishes the session and routes recovery links.
 *
 * AIDEV-NOTE: live verification is gated on the Supabase dashboard registering
 * the `altune://auth/{confirm,recovery,callback}` redirect URLs and the email
 * templates' token style. `parseAuthLink` whitelists by PATH so it's robust to
 * token_hash-vs-access_token; `completeAuthIntent` reads whichever is present.
 */
import * as Linking from 'expo-linking';
import { useRouter } from 'expo-router';
import { useEffect } from 'react';

import { completeAuthIntent } from '../lib/completeAuthIntent';
import { parseAuthLink } from '../lib/parseAuthLink';

export function useAuthDeepLink(): void {
  const router = useRouter();

  useEffect(() => {
    let active = true;

    const handle = (url: string | null): void => {
      if (!url || !active) {
        return;
      }
      void completeAuthIntent(parseAuthLink(url), router);
    };

    void Linking.getInitialURL().then(handle);
    const sub = Linking.addEventListener('url', ({ url }) => handle(url));

    return () => {
      active = false;
      sub.remove();
    };
  }, [router]);
}
