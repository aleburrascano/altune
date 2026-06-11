import { supabase } from '../../auth/api/supabaseClient';
import { apiBase } from '@shared/api-client';

export function audioStreamUrl(trackId: string): string {
  return `${apiBase}/v1/tracks/${trackId}/audio`;
}

export async function audioRequestHeaders(): Promise<Record<string, string>> {
  const { data } = await supabase.auth.getSession();
  const headers: Record<string, string> = {};
  if (data.session?.access_token) {
    headers.Authorization = `Bearer ${data.session.access_token}`;
  }
  return headers;
}
