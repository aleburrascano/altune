import { useEffect } from 'react';

import { supabase } from '@shared/auth/supabaseClient';
import { useNavigator } from '@shared/navigation';
import { showAlert } from '@shared/ui/dialog/dialog';
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
  showAlert(
    "This link didn't work",
    'It may have expired or already been used. Request a new one from the sign-in screen.',
  );
}

async function reportFailedExchange(
  kind: LinkKind,
  exchange: Promise<AuthIntentResult>,
): Promise<void> {
  try {
    reportRefusedLink(kind, await exchange);
  } catch (err) {
    console.warn('[auth] deep link exchange threw', { intent: kind, ...thrownErrorDetail(err) });
    showAlert("Couldn't open that link", 'Check your connection and try opening the link again.');
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

    initialUrl()
      .then(handle)
      .catch((err: unknown) => {
        console.warn('[auth] initial url read failed', thrownErrorDetail(err));
      });
    const unsubscribe = subscribeUrl(handle);

    return () => {
      active = false;
      unsubscribe();
    };
  }, [router]);
}
