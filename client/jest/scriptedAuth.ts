import type { AuthChangeEvent, Session } from '@supabase/supabase-js';
import { act } from '@testing-library/react-native';

import type { installAuthClient } from '@shared/auth';
import { authClientOver } from '@testing/fakeAuthClient';

type AuthListener = (event: AuthChangeEvent, session: Session | null) => void;
export type SignOutOutcome = () => { error: { message: string; status: number } | null };

export type ScriptedAuth = {
  switchTo(session: Session): void;
  emit(event: AuthChangeEvent, session: Session | null): void;
};

export function scriptedAuth(
  install: typeof installAuthClient,
  initial: Session | null,
  signOut: SignOutOutcome = () => ({ error: null }),
): ScriptedAuth {
  let current = initial;
  const listeners = new Set<AuthListener>();
  install(
    authClientOver({
      getSession: async () => ({ data: { session: current }, error: null }),
      onAuthStateChange: (listener: AuthListener) => {
        listeners.add(listener);
        return { data: { subscription: { unsubscribe: () => listeners.delete(listener) } } };
      },
      signOut: async () => signOut(),
    }),
  );
  function emit(event: AuthChangeEvent, session: Session | null): void {
    act(() => {
      listeners.forEach((listener) => listener(event, session));
    });
  }
  return {
    emit,
    switchTo(session) {
      current = session;
      emit('SIGNED_IN', session);
    },
  };
}
