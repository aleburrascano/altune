import { useEffect } from 'react';

import { supabase } from '@shared/auth/supabaseClient';
import { useNavigator } from '@shared/navigation';
import { showFailureAlert } from '@shared/ui/failure/showFailure';
import { type AuthIntentResult, completeAuthIntent } from '@features/auth/domain/completeAuthIntent';
import { thrownErrorDetail } from '@features/auth/domain/errorDetail';
import { initialUrl, subscribeUrl } from '@features/auth/native/linkEvents';
import { type AuthLinkIntent, parseAuthLink } from '@features/auth/domain/parseAuthLink';

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
  showFailureAlert({
    surface: 'auth.deep_link.refused',
    title: "This link didn't work",
    message: 'It may have expired or already been used. Request a new one from the sign-in screen.',
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
    showFailureAlert({
      surface: 'auth.deep_link.threw',
      title: "Couldn't open that link",
      message: 'Check your connection and try opening the link again.',
    });
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
