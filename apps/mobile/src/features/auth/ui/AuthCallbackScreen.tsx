import { Redirect, useRouter } from 'expo-router';
import { useEffect, useState, type ReactElement } from 'react';
import { Platform } from 'react-native';

import { supabase } from '@shared/auth/supabaseClient';
import { Text } from '@shared/ui/primitives/Text';

import { type AuthIntentResult, type AuthRouter, completeAuthIntent } from '../completeAuthIntent';
import { type AuthLinkIntent, parseAuthLink } from '../parseAuthLink';

import { AuthFullScreenNotice } from './AuthFullScreenNotice';
import { BackToSignInLink } from './BackToSignInLink';

type ScreenState = 'pending' | 'error';

function currentUrl(): string | null {
  return Platform.OS === 'web' && typeof window !== 'undefined' ? window.location.href : null;
}

function stripCredentialsFromUrl(): void {
  if (Platform.OS !== 'web' || typeof window === 'undefined') return;
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

async function completeCallback(router: AuthRouter): Promise<'ok' | 'error'> {
  const url = currentUrl();
  stripCredentialsFromUrl();
  if (!url) return 'error';
  const intent = parseAuthLink(url);
  const outcome = await completeAuthIntent(intent, router, supabase.auth);
  return landed(outcome) ? navigateToLanding(intent, router) : 'error';
}

function onMountCompleteCallback(router: AuthRouter, onError: () => void): () => void {
  let active = true;
  completeCallback(router)
    .then((outcome) => (active && outcome === 'error' ? onError() : undefined))
    .catch(() => (active ? onError() : undefined));
  return () => {
    active = false;
  };
}

function useAuthCallbackState(router: AuthRouter): ScreenState {
  const [state, setState] = useState<ScreenState>('pending');
  useEffect(() => onMountCompleteCallback(router, () => setState('error')), [router]);
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

function AuthCallbackErrorBody(): ReactElement {
  return (
    <Text variant="body" tone="secondary" style={{ textAlign: 'center' }}>
      It may have expired or already been used. Request a new one from the sign-in screen.
    </Text>
  );
}

function AuthCallbackError(): ReactElement {
  return (
    <AuthFullScreenNotice testID="auth-callback-error" padded>
      <AuthCallbackErrorHeading />
      <AuthCallbackErrorBody />
      <BackToSignInLink testID="auth-callback-signin" />
    </AuthFullScreenNotice>
  );
}

function AuthCallbackScreenWeb(): ReactElement {
  const router = useRouter();
  const state = useAuthCallbackState(router);
  return state === 'error' ? <AuthCallbackError /> : <AuthCallbackPending />;
}

export function AuthCallbackScreen(): ReactElement {
  return Platform.OS === 'web' ? <AuthCallbackScreenWeb /> : <Redirect href="/" />;
}
