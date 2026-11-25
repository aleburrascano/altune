function originOf(location: Location | undefined): string | null {
  const origin = location?.origin;
  return origin === undefined || origin === '' ? null : origin;
}

export function webOrigin(): string | null {
  return typeof window !== 'undefined' ? originOf(window.location) : null;
}
