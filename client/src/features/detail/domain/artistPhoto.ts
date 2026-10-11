const LASTFM_PLACEHOLDER_HASH = '2a96cbd8b46e442fc41c2b86b821562f';

export function realPhotoUrl(url: string | null | undefined): string | null {
  if (url == null || url === '' || url.includes(LASTFM_PLACEHOLDER_HASH)) return null;
  return url;
}
