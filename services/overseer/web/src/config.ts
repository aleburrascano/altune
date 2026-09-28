
export interface ClientConfig {
  supabaseUrl: string;
  supabaseAnonKey: string;
}

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
