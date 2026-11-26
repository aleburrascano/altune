import { authIntentClient } from '@shared/auth';
import { warn } from '@shared/log';
import { showFailureAlert } from '@shared/ui';
import {
  type AuthIntentResult,
  type AuthRouter,
  completeAuthIntent,
} from '@features/signIn/adapters/completeAuthIntent';
import { thrownErrorDetail } from '@features/signIn/core/authError';
import { type AuthLinkIntent, parseAuthLink } from '@features/signIn/core/parseAuthLink';
import type { LinkEvents } from '@features/signIn/ports/linkEvents';

type LinkKind = AuthLinkIntent['kind'];
type AuthClient = Parameters<typeof completeAuthIntent>[2];

type DeepLinkLog = { warn(message: string, ...details: unknown[]): void };

type AuthDeepLinkDeps = {
  links: LinkEvents;
  router: AuthRouter;
  auth: AuthClient;
  log: DeepLinkLog;
};

function reportRefusedLink(kind: LinkKind, outcome: AuthIntentResult, log: DeepLinkLog): void {
  if (outcome.kind !== 'failure') {
    return;
  }
  log.warn('[auth] deep link exchange failed', {
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
  log: DeepLinkLog,
): Promise<void> {
  try {
    reportRefusedLink(kind, await exchange, log);
  } catch (err) {
    log.warn('[auth] deep link exchange threw', { intent: kind, ...thrownErrorDetail(err) });
    showFailureAlert({
      surface: 'auth.deep_link.threw',
      title: "Couldn't open that link",
      message: 'Check your connection and try opening the link again.',
    });
  }
}

export function startAuthDeepLinks({ links, router, auth, log }: AuthDeepLinkDeps): () => void {
  let active = true;

  const handle = (url: string | null): void => {
    if (!url || !active) {
      return;
    }
    const intent = parseAuthLink(url);
    void reportFailedExchange(intent.kind, completeAuthIntent(intent, router, auth), log);
  };

  links
    .initialUrl()
    .then(handle)
    .catch((err: unknown) => {
      log.warn('[auth] initial url read failed', thrownErrorDetail(err));
    });
  const unsubscribe = links.subscribeUrl(handle);

  return () => {
    active = false;
    unsubscribe();
  };
}

export function startAppAuthDeepLinks(links: LinkEvents, router: AuthRouter): () => void {
  return startAuthDeepLinks({ links, router, auth: authIntentClient(), log: { warn } });
}
