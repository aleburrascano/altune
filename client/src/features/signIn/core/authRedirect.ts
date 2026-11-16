import { declaredAppScheme, webOrigin } from '@shared/device';

export type AuthLinkHost = { scheme: string; origin: string | null };

export function schemePrefix(declared: string | string[] | undefined): string {
  const primary = Array.isArray(declared) ? declared[0] : declared;
  if (typeof primary !== 'string' || primary === '') {
    throw new Error('Missing required Expo config field `scheme` (client/app.json)');
  }
  return `${primary.toLowerCase()}://`;
}

const APP_SCHEME = schemePrefix(declaredAppScheme());

export function appLinkHost(): AuthLinkHost {
  return { scheme: APP_SCHEME, origin: webOrigin() };
}

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

export function authRedirectUrl(
  intent: AuthRedirectIntent,
  host: AuthLinkHost = appLinkHost(),
): string {
  const path = REDIRECT_PATH_FOR_INTENT[intent];
  return host.origin ? `${host.origin}/${path}` : `${host.scheme}${path}`;
}
