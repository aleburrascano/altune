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
      void completeAuthIntent(parseAuthLink(url), router);
    };

    void Linking.getInitialURL().then(handle);
    const sub = Linking.addEventListener('url', ({ url }) => handle(url));

    return () => {
      active = false;
      sub.remove();
    };
  }, [router]);
}
