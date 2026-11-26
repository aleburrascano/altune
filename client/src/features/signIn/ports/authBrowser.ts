export type AuthSessionResult = { type: string; url?: string };

export interface AuthBrowser {
  openAuthSession(url: string, redirectUrl: string): Promise<AuthSessionResult>;
  dismissAuthSession(): void;
}
