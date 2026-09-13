import * as Linking from 'expo-linking';
import { useRouter } from 'expo-router';
import { useEffect } from 'react';

import { completeAuthIntent } from '../lib/completeAuthIntent';
import { parseAuthLink } from '../lib/parseAuthLink';

export function useAuthDeepLink(): void {
  const router = useRouter();

  useEffect(() => {
    let active = true;

    const handle = (url: string | null): void => {
      if (!url || !active) {
        return;
      }
      // A rejected exchange (e.g. the SDK throws on a transport failure) has no
      // UI to surface to from this background listener, but it must not become
      // an unhandled promise rejection — swallow it here.
      void completeAuthIntent(parseAuthLink(url), router).catch(() => undefined);
    };

    void Linking.getInitialURL().then(handle);
    const sub = Linking.addEventListener('url', ({ url }) => handle(url));

    return () => {
      active = false;
      sub.remove();
    };
  }, [router]);
}
