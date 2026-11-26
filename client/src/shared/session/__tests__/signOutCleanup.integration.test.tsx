import type { ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import type { Session } from '@supabase/supabase-js';

import { getSessionExpired, installAuthClient, markSessionExpired, useSession } from '@shared/auth';
import { warn } from '@shared/log';
import { readOwner } from '@shared/offline';
import { hasSignedInUser, onIdentityChange, onSignOut } from '@shared/session';
import {
  createAppTelemetry,
  createOutboxStore,
  enqueueCritical,
  installTelemetry,
  type OutboxStore,
} from '@shared/telemetry';
import { authClientOver } from '@testing/fakeAuthClient';
import { createMemoryFileStore } from '@testing/fakes/files/inMemoryFileStore';

type AuthListener = (event: string, session: Session | null) => void;

const USER_ID = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa';
const OUTBOX_UNREACHABLE = new Error('outbox target unreachable');

function sessionFor(userId: string): Session {
  return { access_token: 't', refresh_token: 'r', user: { id: userId } } as unknown as Session;
}

type WrapperProps = { children: ReactNode };

function wrapper({ children }: WrapperProps) {
  const client = new QueryClient();
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function ignoreAuthEvent(): void {}

function signedOutAtBoot() {
  let emit: AuthListener = ignoreAuthEvent;
  installAuthClient(
    authClientOver({
      getSession: jest.fn().mockResolvedValue({ data: { session: null } }),
      onAuthStateChange: jest.fn((listener: AuthListener) => {
        emit = listener;
        return { data: { subscription: { unsubscribe: jest.fn() } } };
      }),
    }),
  );
  return { emit: (event: string, session: Session | null) => emit(event, session) };
}

async function signIn(auth: ReturnType<typeof signedOutAtBoot>) {
  const { result } = renderHook(() => useSession(), { wrapper });
  await waitFor(() => expect(result.current.status).toBe('signed-out'));
  act(() => {
    auth.emit('SIGNED_IN', sessionFor(USER_ID));
  });
  await waitFor(() => expect(result.current.status).toBe('signed-in'));
  return result;
}

function followingTelemetry(): OutboxStore {
  const store = createOutboxStore(createMemoryFileStore(), { warn });
  function send(): Promise<void> {
    return Promise.reject(OUTBOX_UNREACHABLE);
  }
  const telemetry = createAppTelemetry({ store, send });
  installTelemetry(telemetry);
  telemetry.followSignedInUser();
  return store;
}

describe('identity change wiring on the real modules', () => {
  it('tells identity listeners before it flags signed-in, and downloads and the outbox follow the user', async () => {
    const outbox = followingTelemetry();
    const events: string[] = [];
    onSignOut(() => {
      events.push('forget');
    });
    onIdentityChange((id) => events.push(`identity:${String(id)}:${String(hasSignedInUser())}`));

    await signIn(signedOutAtBoot());

    expect(events).toEqual(['identity:null:false', 'forget', `identity:${USER_ID}:false`]);
    expect(hasSignedInUser()).toBe(true);
    expect(readOwner()).toBe(USER_ID);
    await enqueueCritical({ type: 'library_add' });
    expect(outbox.load().map((entry) => entry.owner_user_id)).toEqual([USER_ID]);
  });

  it('clears the session-expired flag when the identity changes to signed out', async () => {
    followingTelemetry();
    const auth = signedOutAtBoot();
    await signIn(auth);
    markSessionExpired();
    expect(getSessionExpired()).toBe(true);

    act(() => {
      auth.emit('SIGNED_OUT', null);
    });

    await waitFor(() => expect(getSessionExpired()).toBe(false));
  });
});
