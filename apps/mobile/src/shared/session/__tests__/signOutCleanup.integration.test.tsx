import React from 'react';
import * as ReactQuery from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';
import type { Session } from '@supabase/supabase-js';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn() }));

const USER_ID = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa';

function sessionFor(userId: string): Session {
  return { access_token: 't', refresh_token: 'r', user: { id: userId } } as unknown as Session;
}

function bootFreshApp() {
  jest.resetModules();
  jest.doMock('react', () => React);
  jest.doMock('@tanstack/react-query', () => ReactQuery);
  return {
    session: require('@shared/auth/useSession'),
    supabase: require('@shared/auth/supabaseClient').supabase,
    outbox: require('@shared/telemetry/outbox'),
    pinned: require('@shared/offline/pinnedStore'),
    registry: require('@shared/session/signOutCleanup'),
    expired: require('@shared/auth/sessionExpired'),
  };
}

function wrapper({ children }: { children: React.ReactNode }) {
  const client = new ReactQuery.QueryClient();
  return <ReactQuery.QueryClientProvider client={client}>{children}</ReactQuery.QueryClientProvider>;
}

async function signInAfterBootAsSignedOut(app: ReturnType<typeof bootFreshApp>) {
  app.supabase.auth.getSession = jest.fn().mockResolvedValue({ data: { session: null } });
  app.supabase.auth.onAuthStateChange = jest.fn((listener) => {
    setTimeout(() => listener('SIGNED_IN', sessionFor(USER_ID)), 0);
    return { data: { subscription: { unsubscribe: jest.fn() } } };
  });
  const { result } = renderHook(() => app.session.useSession(), { wrapper });
  await waitFor(() => expect(result.current.status).toBe('signed-in'));
}

describe('identity change wiring on the real modules', () => {
  it('forgets, then hands the outbox to the user, then claims downloads, then flags signed-in', async () => {
    const app = bootFreshApp();
    const events: string[] = [];
    app.registry.onSignOut(() => events.push('forget'));
    const outboxOwner = jest.spyOn(app.outbox, 'setOutboxOwner');
    const claim = jest.spyOn(app.pinned, 'claimPinnedDownloads');
    outboxOwner.mockImplementation(() => events.push('outbox'));
    claim.mockImplementation(() => events.push(`claim:${String(app.registry.hasSignedInUser())}`));
    await signInAfterBootAsSignedOut(app);
    expect(events).toEqual(['outbox', 'forget', 'outbox', 'claim:false']);
    expect(app.registry.hasSignedInUser()).toBe(true);
  });

  it('clears the session-expired flag when the identity changes to signed out', async () => {
    const app = bootFreshApp();
    app.expired.markSessionExpired();
    expect(app.expired.getSessionExpired()).toBe(true);
    const { forgetPreviousUsersLocalData } = require('@shared/auth/forgetPreviousUsersLocalData');
    forgetPreviousUsersLocalData(new ReactQuery.QueryClient());
    expect(app.expired.getSessionExpired()).toBe(false);
  });
});
