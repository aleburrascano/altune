/**
 * Joins a path to a `URLSearchParams` query, appending the `?` only when at
 * least one parameter is present. The single home for the "no bare `?` when the
 * query is empty" idiom every typed api-client function otherwise hand-rolls —
 * and, unlike the deleted `_contentUrl` string-interpolation, it always encodes
 * through `URLSearchParams`.
 */
export function withQuery(path: string, params: URLSearchParams): string {
  const qs = params.toString();
  return qs ? `${path}?${qs}` : path;
}
