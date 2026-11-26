import * as Linking from 'expo-linking';

import { isWebPlatform } from '@shared/device';
import type { LinkEvents } from '@features/signIn/ports/linkEvents';

export function initialUrl(): Promise<string | null> {
  return Linking.getInitialURL();
}

export function subscribeUrl(listener: (url: string) => void): () => void {
  const subscription = Linking.addEventListener('url', ({ url }) => listener(url));
  return () => subscription.remove();
}

function initialAppUrl(): Promise<string | null> {
  return isWebPlatform() ? Promise.resolve(null) : initialUrl();
}

function subscribeAppUrl(listener: (url: string) => void): () => void {
  return isWebPlatform() ? () => {} : subscribeUrl(listener);
}

export const nativeLinkEvents: LinkEvents = {
  initialUrl: initialAppUrl,
  subscribeUrl: subscribeAppUrl,
};
