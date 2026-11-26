export type AuthUser = { readonly id: string; readonly email?: string | undefined };

export type AuthSession = { readonly user: AuthUser };

export type AuthStateListener = (event: string, session: AuthSession | null) => void;

export type SessionRead = { data: { session: AuthSession | null } };

export type AuthSubscription = { data: { subscription: { unsubscribe(): void } } };

export type SignOutOptions = { scope: 'local' };

export type SignOutReply = { error: unknown };

export interface AuthSource {
  getSession(): Promise<SessionRead>;
  onAuthStateChange(listener: AuthStateListener): AuthSubscription;
  signOut(options?: SignOutOptions): Promise<SignOutReply>;
  clearPersistedSession(): Promise<void>;
}

let provided: AuthSource | undefined;

export function provideAuthSource(source: AuthSource): void {
  provided = source;
}

export function authSource(): AuthSource {
  if (provided === undefined) {
    throw new Error('auth has no source: the auth adapter provides one when it loads');
  }
  return provided;
}
