export function schemePrefix(declared: string | string[] | undefined): string {
  const primary = Array.isArray(declared) ? declared[0] : declared;
  if (typeof primary !== 'string' || primary === '') {
    throw new Error('Missing required Expo config field `scheme` (client/app.json)');
  }
  return `${primary.toLowerCase()}://`;
}
