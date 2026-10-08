import { declaredAppScheme, webOrigin } from '@shared/device/device';

function configuredSchemePrefix(): string {
  const declared = declaredAppScheme();
  const primary = Array.isArray(declared) ? declared[0] : declared;
  if (typeof primary !== 'string' || primary === '') {
    throw new Error('Missing required Expo config field `scheme` (apps/mobile/app.json)');
  }
  return `${primary.toLowerCase()}://`;
}

export const SCHEME = configuredSchemePrefix();

export const LINK_PATH = {
  recovery: 'auth/recovery',
  confirm: 'auth/confirm',
  oauth: 'auth/callback',
} as const;

export type AuthRedirectIntent = 'callback' | 'confirm' | 'recovery';

const REDIRECT_PATH_FOR_INTENT: Record<AuthRedirectIntent, string> = {
  callback: LINK_PATH.oauth,
  confirm: LINK_PATH.confirm,
  recovery: LINK_PATH.recovery,
};

export function authRedirectUrl(intent: AuthRedirectIntent): string {
  const path = REDIRECT_PATH_FOR_INTENT[intent];
  const origin = webOrigin();
  return origin ? `${origin}/${path}` : `${SCHEME}${path}`;
}
