import { useEffect } from 'react';

import { supabase } from '@shared/auth/supabaseClient';
import { useNavigator } from '@shared/navigation';
import { type AuthIntentResult, completeAuthIntent } from '../completeAuthIntent';
import { thrownErrorDetail } from '../errorDetail';
import { initialUrl, subscribeUrl } from '../native/linkEvents';
import { type AuthLinkIntent, parseAuthLink } from '../parseAuthLink';

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
  const router = useNavigator();

  useEffect(() => {
    let active = true;

    const handle = (url: string | null): void => {
      if (!url || !active) {
        return;
      }
      const intent = parseAuthLink(url);
      void reportFailedExchange(intent.kind, completeAuthIntent(intent, router, supabase.auth));
    };

    void initialUrl().then(handle);
    const unsubscribe = subscribeUrl(handle);

    return () => {
      active = false;
      unsubscribe();
    };
  }, [router]);
}
