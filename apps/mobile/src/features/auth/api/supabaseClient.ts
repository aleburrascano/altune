/**
 * Re-export from shared/auth — the singleton was promoted because 2+ features
 * depend on it. This re-export keeps existing auth-internal imports stable.
 */
export { supabase } from '@shared/auth/supabaseClient';
