import * as Linking from 'expo-linking';

import { isWebPlatform } from '@shared/device';
import type { LinkEvents } from '@features/signIn/ports/linkEvents';

export interface UrlEvent {
  url: string;
}

export interface LinkSubscription {
  remove(): void;
}

export interface LinkingSource {
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

export const nativeLinkEvents: LinkEvents = createLinkEvents(Linking);
