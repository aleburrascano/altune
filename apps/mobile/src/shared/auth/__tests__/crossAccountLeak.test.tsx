import React from 'react';
import type { Session } from '@supabase/supabase-js';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, render, act, waitFor, screen } from '@testing-library/react-native';

import {
  linkTrackIdentity,
  patchTrackStatus,
  trackIdentityKey,
  useTrackStatusStore,
} from '@shared/acquisition/trackStatusStore';
import { enqueueCritical, flushOutbox, _resetOutboxForTest } from '@shared/telemetry/outbox';
import { useOwnedTrack } from '@features/detail/hooks/useOwnedTrack';
import type { TrackExtras } from '@features/detail/extras-accessors';
import { TrackSaveControl } from '@features/detail/ui/TrackSaveControl';

import { useSession } from '../useSession';
import { supabase } from '../supabaseClient';

const { __http } = require('../../../../jest/doubles/fetch.js');

type AuthCallback = (event: string, session: Session | null) => void;

const USER_A = { id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa' } as Session['user'];
const USER_B = { id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb' } as Session['user'];

const EVENTS = 'POST /v1/discovery/events';

function sessionFor(user: Session['user'], token: string): Session {
  return {
    access_token: token,
    refresh_token: `${token}-refresh`,
    expires_at: Math.floor(Date.now() / 1000) + 3600,
    expires_in: 3600,
    token_type: 'bearer',
    user,
  } as Session;
}

function stampedElsewhere(): TrackExtras {
  return { trackId: null, acquisitionStatus: null } as unknown as TrackExtras;
}

function makeWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

let authCallbacks: AuthCallback[] = [];

function bootSignedIn(initial: Session): void {
  jest
    .spyOn(supabase.auth, 'getSession')
    .mockResolvedValue({ data: { session: initial }, error: null } as never);
  jest.spyOn(supabase.auth, 'onAuthStateChange').mockImplementation(((cb: AuthCallback) => {
    authCallbacks.push(cb);
    return { data: { subscription: { unsubscribe: jest.fn() } } };
  }) as never);
}

function switchAccountTo(session: Session): void {
  jest
    .spyOn(supabase.auth, 'getSession')
    .mockResolvedValue({ data: { session }, error: null } as never);
  act(() => {
    authCallbacks.forEach((cb) => cb('SIGNED_IN', session));
  });
}

beforeEach(() => {
  authCallbacks = [];
  jest.restoreAllMocks();
  _resetOutboxForTest();
  act(() => {
    useTrackStatusStore.getState().reset();
  });
});

describe('cross-account acquisition-status leak on a shared device (#676)', () => {
  it('after A saves a track and B signs in, useOwnedTrack no longer resolves A save status for a shared identity', async () => {
    bootSignedIn(sessionFor(USER_A, 'token-a'));
    const queryClient = new QueryClient();
    const session = renderHook(() => useSession(), { wrapper: makeWrapper(queryClient) });
    await waitFor(() => expect(session.result.current.status).toBe('signed-in'));

    // User A saves a track: the identity store links the normalized key to A's trackId.
    const identity = trackIdentityKey('Song Title', 'The Artist');
    patchTrackStatus('track-owned-by-a', { acquisitionStatus: 'ready', failureMessage: null });
    linkTrackIdentity(identity, 'track-owned-by-a');

    const owned = renderHook(() =>
      useOwnedTrack(stampedElsewhere(), { title: 'Song Title', artist: 'The Artist' }),
    );
    // Sanity: while A is signed in, the shared identity resolves to A's saved track.
    expect(owned.result.current).toEqual({
      trackId: 'track-owned-by-a',
      acquisitionStatus: 'ready',
    });

    // A signs out; B signs in on the same device.
    switchAccountTo(sessionFor(USER_B, 'token-b'));

    expect(owned.result.current).toBeNull();
    expect(useTrackStatusStore.getState().statuses).toEqual({});
    expect(useTrackStatusStore.getState().identities).toEqual({});

    session.unmount();
  });

  it("TrackSaveControl stops showing A's 'in library' state and reverts to a savable control once B signs in", async () => {
    bootSignedIn(sessionFor(USER_A, 'token-a'));
    const queryClient = new QueryClient();
    const session = renderHook(() => useSession(), { wrapper: makeWrapper(queryClient) });
    await waitFor(() => expect(session.result.current.status).toBe('signed-in'));

    patchTrackStatus('track-owned-by-a', { acquisitionStatus: 'ready', failureMessage: null });
    linkTrackIdentity(trackIdentityKey('Song Title', 'The Artist'), 'track-owned-by-a');

    render(
      <TrackSaveControl
        state="add"
        onPress={jest.fn()}
        title="Song Title"
        artist="The Artist"
      />,
    );
    // Before the switch, the control reflects A's saved track for B's shared identity.
    expect(screen.getByLabelText('Song Title in library')).toBeTruthy();

    switchAccountTo(sessionFor(USER_B, 'token-b'));

    expect(screen.queryByLabelText('Song Title in library')).toBeNull();
    expect(screen.getByLabelText('Save Song Title')).toBeTruthy();

    session.unmount();
  });
});

describe('cross-account queued-telemetry leak on a shared device (#676)', () => {
  it("a critical entry A queued while offline is dropped on account switch, never sent under B's session", async () => {
    bootSignedIn(sessionFor(USER_A, 'token-a'));
    const queryClient = new QueryClient();
    const session = renderHook(() => useSession(), { wrapper: makeWrapper(queryClient) });
    await waitFor(() => expect(session.result.current.status).toBe('signed-in'));

    // A files a wrong_album report while the network is down: it lands in the outbox.
    __http.fail(EVENTS);
    await act(async () => {
      await enqueueCritical({ type: 'wrong_album', payload: { title: 'Song', artist: 'Artist' } });
    });
    const attemptsAsA = __http.countFor(EVENTS);
    expect(attemptsAsA).toBeGreaterThanOrEqual(1);

    // A signs out; B signs in on the same device.
    switchAccountTo(sessionFor(USER_B, 'token-b'));

    // The network is back; a foreground flush would send anything still queued.
    __http.reply(EVENTS, { status: 202 });
    await act(async () => {
      await flushOutbox();
    });

    // Nothing new was sent — A's report was dropped, not misattributed to B.
    expect(__http.countFor(EVENTS)).toBe(attemptsAsA);

    session.unmount();
  });
});
