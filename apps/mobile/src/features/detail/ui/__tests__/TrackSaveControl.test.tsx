import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react-native';

import type { CreateTrackRequest } from '@shared/api-client/types';
import { supabase } from '@shared/auth/supabaseClient';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';

import { useSaveTrack } from '../../hooks/useSaveTrack';
import { TrackSaveControl } from '../TrackSaveControl';

const { __http } = require('../../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

function freshClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

const TITLE = 'Midnight City';
const ARTIST = 'M83';

// Pressable's onPress calls e.stopPropagation(); fireEvent doesn't synthesize an
// event, so hand it one.
const pressEvent = { stopPropagation: () => {} };

function request(): CreateTrackRequest {
  return {
    title: TITLE,
    artist: ARTIST,
    album: null,
    duration_seconds: null,
    artwork_url: null,
    isrc: null,
    year: null,
    genre: null,
    album_artist: null,
    track_number: null,
    source_url: null,
  };
}

// A row's quick-save control wired to a real save mutation, the same shape as
// the album/artist detail rows: onPress fires save.mutate for this track.
function QuickSaveRow(): React.ReactElement {
  const save = useSaveTrack();
  return (
    <TrackSaveControl
      testID="quick-save"
      state="add"
      title={TITLE}
      artist={ARTIST}
      onPress={() => save.mutate(request())}
    />
  );
}

function requestFor(title: string, artist: string): CreateTrackRequest {
  return { ...request(), title, artist };
}

function SaveRow({ title, artist }: { title: string; artist: string }): React.ReactElement {
  const save = useSaveTrack();
  return (
    <TrackSaveControl
      testID="quick-save"
      state="add"
      title={title}
      artist={artist}
      onPress={() => save.mutate(requestFor(title, artist))}
    />
  );
}

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
  useTrackStatusStore.getState().reset();
});

afterEach(() => {
  useTrackStatusStore.getState().reset();
});

describe('TrackSaveControl quick-save re-entrancy', () => {
  it('fires a single createTrack POST when the save glyph is double-tapped before the mutation settles', async () => {
    // The request resolves (as a server error), but the batched taps below both
    // fire before the mutation settles, so the outcome is irrelevant — only how
    // many POSTs the double-tap produced.
    __http.reply('POST /v1/tracks', { status: 500 });
    const queryClient = freshClient();
    render(<QuickSaveRow />, { wrapper: createWrapper(queryClient) });

    // Both releases land on the same render, before the first save's status has
    // flushed back through the store — the real fast-double-tap race. Batching
    // them in one act() keeps that flush from happening between the taps.
    const control = screen.getByTestId('quick-save');
    act(() => {
      fireEvent.press(control, pressEvent);
      fireEvent.press(control, pressEvent);
    });

    await waitFor(() => expect(__http.countFor('POST /v1/tracks')).toBeGreaterThan(0));
    expect(__http.countFor('POST /v1/tracks')).toBe(1);
  });
});

describe('TrackSaveControl identity collision', () => {
  // "Encore" / "Jay Z Interlude" and "Encore Jay Z" / "Interlude" are different
  // tracks that both space-join to "encore jay z interlude". A plain-space
  // identity key lets the second, unsaved track inherit the first's saved status.
  const SAVED_TITLE = 'Encore';
  const SAVED_ARTIST = 'Jay Z Interlude';
  const OTHER_TITLE = 'Encore Jay Z';
  const OTHER_ARTIST = 'Interlude';

  function trackResponse() {
    return {
      id: 'srv-encore',
      title: SAVED_TITLE,
      artist: SAVED_ARTIST,
      album: null,
      duration_seconds: null,
      added_at: '2024-01-01T00:00:00Z',
      acquisition_status: 'ready',
      artwork_url: null,
      failure_reason: null,
      year: null,
      genre: null,
      track_number: null,
      album_artist: null,
      isrc: null,
      audio_ref: null,
    };
  }

  it('does not show a different, unsaved track as saved when its title/artist space-collides', async () => {
    __http.reply('POST /v1/tracks', { status: 200, json: trackResponse() });
    const queryClient = freshClient();

    // Save the first track and let its saved ("in library") status settle.
    const saved = render(<SaveRow title={SAVED_TITLE} artist={SAVED_ARTIST} />, {
      wrapper: createWrapper(queryClient),
    });
    await act(async () => {
      fireEvent.press(screen.getByTestId('quick-save'), pressEvent);
    });
    await waitFor(() =>
      expect(screen.getByLabelText(`${SAVED_TITLE} in library`)).toBeTruthy(),
    );
    saved.unmount();

    // A different, never-saved track whose fields space-collide with the saved
    // one must still render as savable — not inherit the saved status. The
    // zustand identity/status store survives the unmount above.
    render(<SaveRow title={OTHER_TITLE} artist={OTHER_ARTIST} />, {
      wrapper: createWrapper(queryClient),
    });

    expect(screen.getByLabelText(`Save ${OTHER_TITLE}`)).toBeTruthy();
    expect(screen.queryByLabelText(`${OTHER_TITLE} in library`)).toBeNull();
  });
});

describe('TrackSaveControl quick-save failure', () => {
  it('surfaces a retry/failure state on the row when the save mutation fails', async () => {
    __http.fail('POST /v1/tracks');
    const queryClient = freshClient();
    render(<QuickSaveRow />, { wrapper: createWrapper(queryClient) });

    await act(async () => {
      fireEvent.press(screen.getByTestId('quick-save'), pressEvent);
    });

    await waitFor(() =>
      expect(screen.getByLabelText(`Retry saving ${TITLE}`)).toBeTruthy(),
    );
    // And it never silently reverts to the plain "add" affordance.
    expect(screen.queryByLabelText(`Save ${TITLE}`)).toBeNull();
  });
});
