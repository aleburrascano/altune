import { isWebPlatform } from '@shared/device';
import type { LinkEvents } from '@features/signIn/ports/ports';

interface UrlEvent {
  url: string;
}

interface LinkSubscription {
  remove(): void;
}

interface LinkingSource {
  getInitialURL(): Promise<string | null>;
  addEventListener(type: 'url', onUrl: (event: UrlEvent) => void): LinkSubscription;
}

export function createLinkEvents(linking: LinkingSource): LinkEvents {
  return {
    initialUrl: () => (isWebPlatform() ? Promise.resolve(null) : linking.getInitialURL()),
    subscribeUrl: (listener) => {
      if (isWebPlatform()) return () => {};
      const subscription = linking.addEventListener('url', ({ url }) => listener(url));
      return () => subscription.remove();
    },
  };
}
