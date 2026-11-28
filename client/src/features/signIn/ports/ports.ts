import type { SignInFailureReason } from '@features/signIn/core/errors';
import type { AuthIntentResult } from '@features/signIn/core/authIntent';
import type { AuthLinkIntent } from '@features/signIn/core/parseAuthLink';

export interface AuthPort {
  reportSignInFailure(reason: SignInFailureReason): void;
}

export type AuthSessionResult = { type: string; url?: string };

export interface AuthBrowser {
  openAuthSession(url: string, redirectUrl: string): Promise<AuthSessionResult>;
  dismissAuthSession(): void;
}

export type LockoutAction = 'sign-in' | 'reset-request';

export type LockedOut = { kind: 'error'; reason: 'too_many_attempts' };

export interface AttemptLockout {
  guard<A extends [string, ...unknown[]], R extends { kind: string }>(
    action: LockoutAction,
    attempt: (...args: A) => Promise<R>,
    countsAsFailure: (outcome: R) => boolean,
  ): (...args: A) => Promise<R | LockedOut>;
}

export type AuthRouter = { replace(href: '/library' | '/reset-password'): void };

export interface AuthIntents {
  complete(intent: AuthLinkIntent, router: AuthRouter): Promise<AuthIntentResult>;
}

export interface LinkEvents {
  initialUrl(): Promise<string | null>;
  subscribeUrl(listener: (url: string) => void): () => void;
}
