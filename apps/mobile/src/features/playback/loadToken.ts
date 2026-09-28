let loadToken = 0;

export function claimLoad(): number {
  return ++loadToken;
}

export function isStale(token: number): boolean {
  return token !== loadToken;
}

export function currentLoadToken(): number {
  return loadToken;
}

export function claimSessionReset(): void {
  claimLoad();
}
