import * as Linking from 'expo-linking';

export function initialUrl(): Promise<string | null> {
  return Linking.getInitialURL();
}

export function subscribeUrl(listener: (url: string) => void): () => void {
  const subscription = Linking.addEventListener('url', ({ url }) => listener(url));
  return () => subscription.remove();
}
