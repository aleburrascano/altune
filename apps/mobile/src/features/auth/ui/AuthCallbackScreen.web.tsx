import { useRouter } from 'expo-router';
import { useEffect, useState, type ReactElement } from 'react';

import { supabase } from '@shared/auth/supabaseClient';
import { NetworkError } from '@shared/errors';
import { Text } from '@shared/ui/primitives/Text';

import {
  type AuthIntentResult,
  type AuthRouter,
  completeAuthIntent,
} from '@features/auth/completeAuthIntent';
import { thrownErrorDetail } from '@features/auth/errorDetail';
import { type AuthLinkIntent, parseAuthLink } from '@features/auth/parseAuthLink';

import { AuthFullScreenNotice } from '@features/auth/ui/AuthFullScreenNotice';
import { BackToSignInLink } from '@features/auth/ui/BackToSignInLink';

type FailureState = 'refused' | 'unreachable';
type ScreenState = 'pending' | FailureState;

function currentUrl(): string | null {
  return typeof window !== 'undefined' ? window.location.href : null;
}

function stripCredentialsFromUrl(): void {
  if (typeof window === 'undefined') return;
  window.history.replaceState(null, '', window.location.pathname);
}

function landingRouteFor(kind: AuthLinkIntent['kind']): '/library' | null {
  return kind === 'oauth' || kind === 'confirm' ? '/library' : null;
}

function landed(outcome: AuthIntentResult): boolean {
  return outcome.kind === 'success' || outcome.kind === 'deduped';
}

function navigateToLanding(intent: AuthLinkIntent, router: AuthRouter): 'ok' {
  const route = landingRouteFor(intent.kind);
  if (route) router.replace(route);
  return 'ok';
}

async function completeCallback(router: AuthRouter): Promise<'ok' | 'refused'> {
  const url = currentUrl();
  stripCredentialsFromUrl();
  if (!url) {
    console.warn('[auth] web callback has no url to read');
    return 'refused';
  }
  const intent = parseAuthLink(url);
  const outcome = await completeAuthIntent(intent, router, supabase.auth);
  if (landed(outcome)) return navigateToLanding(intent, router);
  if (outcome.kind === 'failure') {
    console.warn('[auth] web callback exchange failed', {
      intent: intent.kind,
      cause: outcome.cause,
      error: outcome.error,
    });
  }
  return 'refused';
}

function failureStateFor(err: unknown): FailureState {
  return err instanceof NetworkError ? 'unreachable' : 'refused';
}

function onMountCompleteCallback(
  router: AuthRouter,
  onFailure: (state: FailureState) => void,
): () => void {
  let active = true;
  completeCallback(router)
    .then((outcome) => (active && outcome === 'refused' ? onFailure('refused') : undefined))
    .catch((err: unknown) => {
      console.warn('[auth] web callback exchange threw', thrownErrorDetail(err));
      if (active) onFailure(failureStateFor(err));
    });
  return () => {
    active = false;
  };
}

function useAuthCallbackState(router: AuthRouter): ScreenState {
  const [state, setState] = useState<ScreenState>('pending');
  useEffect(() => onMountCompleteCallback(router, setState), [router]);
  return state;
}

function AuthCallbackPending(): ReactElement {
  return (
    <AuthFullScreenNotice testID="auth-callback-pending">
      <Text variant="label" tone="tertiary">
        Signing you in…
      </Text>
    </AuthFullScreenNotice>
  );
}

function AuthCallbackErrorHeading(): ReactElement {
  return (
    <Text variant="displayL" style={{ textAlign: 'center' }}>
      This link didn&apos;t work
    </Text>
  );
}

const ERROR_COPY: Record<FailureState, string> = {
  refused: 'It may have expired or already been used. Request a new one from the sign-in screen.',
  unreachable: 'We could not reach the server. Check your connection and open the link again.',
};

function AuthCallbackErrorBody({ state }: { state: FailureState }): ReactElement {
  return (
    <Text variant="body" tone="secondary" style={{ textAlign: 'center' }}>
      {ERROR_COPY[state]}
    </Text>
  );
}

function AuthCallbackError({ state }: { state: FailureState }): ReactElement {
  return (
    <AuthFullScreenNotice testID="auth-callback-error" padded>
      <AuthCallbackErrorHeading />
      <AuthCallbackErrorBody state={state} />
      <BackToSignInLink testID="auth-callback-signin" />
    </AuthFullScreenNotice>
  );
}

export function AuthCallbackScreen(): ReactElement {
  const router = useRouter();
  const state = useAuthCallbackState(router);
  return state === 'pending' ? <AuthCallbackPending /> : <AuthCallbackError state={state} />;
}
