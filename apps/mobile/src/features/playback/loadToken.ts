export type LoadToken = number & { readonly __brand: 'LoadToken' };

let loadToken = 0;

export function claimLoad(): LoadToken {
  return ++loadToken as LoadToken;
}

export function isStale(token: LoadToken): boolean {
  return token !== loadToken;
}

export function currentLoadToken(): LoadToken {
  return loadToken as LoadToken;
}

export function claimSessionReset(): void {
  claimLoad();
}
