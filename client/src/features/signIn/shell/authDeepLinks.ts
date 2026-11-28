import { warn } from '@shared/log';
import { showFailureAlert } from '@shared/ui';
import type { AuthIntentResult } from '@features/signIn/core/authIntent';
import { thrownErrorDetail } from '@features/signIn/core/errors';
import { type AuthLinkIntent, parseAuthLink } from '@features/signIn/core/parseAuthLink';
import type { AuthIntents, AuthRouter, LinkEvents } from '@features/signIn/ports/ports';
import { isPresent } from '@features/signIn/core/isPresent';

type LinkKind = AuthLinkIntent['kind'];

function reportRefusedLink(kind: LinkKind, outcome: AuthIntentResult): void {
  if (outcome.kind !== 'failure') {
    return;
  }
  warn('[auth] deep link exchange failed', {
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
    warn('[auth] deep link exchange threw', { intent: kind, ...thrownErrorDetail(err) });
    showFailureAlert({
      surface: 'auth.deep_link.threw',
      title: "Couldn't open that link",
      message: 'Check your connection and try opening the link again.',
    });
  }
}

export function startAppAuthDeepLinks(
  links: LinkEvents,
  router: AuthRouter,
  intents: AuthIntents,
): () => void {
  let active = true;

  const handle = (url: string | null): void => {
    if (!isPresent(url) || !active) {
      return;
    }
    const intent = parseAuthLink(url);
    void reportFailedExchange(intent.kind, intents.complete(intent, router));
  };

  links
    .initialUrl()
    .then(handle)
    .catch((err: unknown) => {
      warn('[auth] initial url read failed', thrownErrorDetail(err));
    });
  const unsubscribe = links.subscribeUrl(handle);

  return () => {
    active = false;
    unsubscribe();
  };
}
