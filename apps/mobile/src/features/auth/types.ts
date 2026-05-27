/**
 * Auth-feature types — trimmed re-exports of the Supabase SDK's surface that
 * altune actually uses. Keeps the rest of the mobile codebase from importing
 * `@supabase/supabase-js` directly.
 */
export type { Session, User } from '@supabase/supabase-js';

/**
 * `useSession`'s exposed state, modeled as a discriminated union per
 * `.claude/rules/typescript-frontend.md`. Components branch on `status`
 * without checking nullable fields.
 */
export type SessionState =
  | { status: 'loading' }
  | { status: 'signed-in'; session: import('@supabase/supabase-js').Session }
  | { status: 'signed-out' };
