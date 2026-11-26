export type NavigationBarScheme = 'light' | 'dark';

export function navigationBarStyleFor(scheme: NavigationBarScheme): NavigationBarScheme {
  return scheme === 'dark' ? 'light' : 'dark';
}
