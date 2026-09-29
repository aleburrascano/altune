import * as WebBrowser from 'expo-web-browser';

WebBrowser.maybeCompleteAuthSession();

export function openAuthSession(
  url: string,
  redirectUrl: string,
): Promise<{ type: string; url?: string }> {
  return WebBrowser.openAuthSessionAsync(url, redirectUrl);
}
