import * as Linking from 'expo-linking';
import { useRouter } from 'expo-router';
import { useEffect } from 'react';
import { Platform } from 'react-native';

import { supabase } from '@shared/auth/supabaseClient';
import { type AuthIntentResult, completeAuthIntent } from '../completeAuthIntent';
import { thrownErrorDetail } from '../errorDetail';
import { type AuthLinkIntent, parseAuthLink } from '../parseAuthLink';

// The link's kind, never the link: what these take is what they may log, so the
// credential in `intent.params` is not theirs to leak (#1647).
type LinkKind = AuthLinkIntent['kind'];

function reportRefusedLink(kind: LinkKind, outcome: AuthIntentResult): void {
  if (outcome.kind !== 'failure') {
    return;
  }
  console.warn('[auth] deep link exchange failed', {
    intent: kind,
    cause: outcome.cause,
    error: outcome.error,
  });
}

// This listener has no UI to report to, so a link that dies here is exactly the
// failure a support ticket is opened about — the user tapped a confirm or reset
// link and nothing happened — and the log is its only trace.
async function reportFailedExchange(
  kind: LinkKind,
  exchange: Promise<AuthIntentResult>,
): Promise<void> {
  try {
    reportRefusedLink(kind, await exchange);
  } catch (err) {
    console.warn('[auth] deep link exchange threw', { intent: kind, ...thrownErrorDetail(err) });
  }
}

export function useAuthDeepLink(): void {
  const router = useRouter();

  useEffect(() => {
    if (Platform.OS === 'web') {
      return;
    }
    let active = true;

    const handle = (url: string | null): void => {
      if (!url || !active) {
        return;
      }
      const intent = parseAuthLink(url);
      // A rejected exchange (e.g. the SDK throws on a transport failure) must
      // not become an unhandled promise rejection — the report absorbs it.
      void reportFailedExchange(intent.kind, completeAuthIntent(intent, router, supabase.auth));
    };

    void Linking.getInitialURL().then(handle);
    const sub = Linking.addEventListener('url', ({ url }) => handle(url));

    return () => {
      active = false;
      sub.remove();
    };
  }, [router]);
}
