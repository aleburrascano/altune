// Runtime client configuration, fetched from the binary's open /config.json so the
// image stays environment-agnostic (the public Supabase URL + anon key are not
// baked into the build). Both values are public client values, safe in the browser.

export interface ClientConfig {
  supabaseUrl: string;
  supabaseAnonKey: string;
}

// base is the app mount prefix Vite was built with ("/overseer/" in prod, "/" in
// dev). API and config paths are resolved relative to it so they land inside the
// Caddy mount, which strips the prefix before proxying to the binary.
export const base = import.meta.env.BASE_URL;

export function apiURL(path: string): string {
  return `${base}${path.replace(/^\//, "")}`;
}

export async function loadConfig(): Promise<ClientConfig> {
  const res = await fetch(apiURL("config.json"), { credentials: "omit" });
  if (!res.ok) {
    throw new Error(`config.json: HTTP ${res.status}`);
  }
  return (await res.json()) as ClientConfig;
}
