// Client-side route paths for the app. The paths are base-relative — the router's
// basename (Vite's `/overseer/` in prod, `/` in dev) is applied by React Router, so
// these stay clean and the app respects the mount prefix without hard-coding it.

export const overviewPath = "/";

// bucketPath is the drill-down route for a single bucket, keyed by its id.
export function bucketPath(id: string): string {
  return `/bucket/${encodeURIComponent(id)}`;
}

// routerBasename normalizes Vite's BASE_URL into a React Router basename: a leading
// slash, no trailing slash ("/overseer/" -> "/overseer", "/" -> "/").
export function routerBasename(baseURL: string): string {
  const trimmed = baseURL.replace(/\/+$/, "");
  return trimmed === "" ? "/" : trimmed;
}
