import React from 'react';
import { StyleSheet, Text } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react-native';

import type { CreateTrackRequest } from '@shared/api-client/types';
import type { TrackId } from '@shared/api-client/ids';
import { supabase } from '@shared/auth/supabaseClient';
import {
  linkTrackIdentity,
  patchTrackStatus,
  trackIdentityKey,
  useTrackStatusStore,
} from '@shared/acquisition/trackStatusStore';

import { useSaveTrack } from '../../hooks/useSaveTrack';
import {
  useResolvedOwnedTrack,
  type OwnedTrack,
  type TrackIdentity,
} from '../../hooks/useOwnedTrack';
import { saveControlLabel, saveControlState } from '../../save-control-state';
import { TrackSaveControl } from '../TrackSaveControl';

const { __http } = require('../../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));
jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));

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
      owned={null}
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
      owned={null}
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
    // The successful save enqueues library_add telemetry; accept it so the outbox
    // does not arm a retry timer that fires after this file has finished.
    __http.reply('POST /v1/discovery/events', { status: 202 });
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

// A "Save all" run holds every track it claimed, dispatched or still queued. A row
// that still offers its own save lets the user write the same track twice (#1658).
describe('TrackSaveControl under a Save all run', () => {
  function ClaimedRow(): React.ReactElement {
    const save = useSaveTrack();
    return (
      <TrackSaveControl
        testID="quick-save"
        owned={null}
        title={TITLE}
        artist={ARTIST}
        savingInBatch
        onPress={() => save.mutate(request())}
      />
    );
  }

  it('does not write the track again when its claimed row is pressed', async () => {
    __http.reply('POST /v1/tracks', { status: 500 });
    render(<ClaimedRow />, { wrapper: createWrapper(freshClient()) });

    await act(async () => {
      fireEvent.press(screen.getByTestId('quick-save'), pressEvent);
    });

    expect(__http.countFor('POST /v1/tracks')).toBe(0);
  });

  it('reads as already saving rather than as an untouched track', () => {
    render(<ClaimedRow />, { wrapper: createWrapper(freshClient()) });

    expect(screen.getByLabelText(`${TITLE} downloading`)).toBeTruthy();
    expect(screen.queryByLabelText(`Save ${TITLE}`)).toBeNull();
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

// Regression for #748: a stamped-owned row and the shared owned-track rule used
// by the detail rows must give the save control the SAME answer. Previously the
// control ignored the row's stamped extras and resolved purely by the
// (title, artist) identity link, so a stale/foreign link to a different trackId
// made the control disagree with the detail row for the very same track.
describe('TrackSaveControl stamped-owned vs identity-only agreement', () => {
  const OWNED_TITLE = 'Ivy';
  const OWNED_ARTIST = 'Frank Ocean';
  const STAMPED_ID = 'stamped-ivy' as TrackId;
  const FOREIGN_ID = 'foreign-ivy' as TrackId;

  const stamped: OwnedTrack = { trackId: STAMPED_ID, acquisitionStatus: 'ready' };
  const identity: TrackIdentity = { title: OWNED_TITLE, artist: OWNED_ARTIST };

  // The detail-row code path: the shared rule fed the row's own stamped extras.
  function DetailRowProbe(): React.ReactElement {
    const owned = useResolvedOwnedTrack(stamped, identity);
    return <Text>{saveControlState(owned)}</Text>;
  }

  it('shows the row\'s own stamped status, not a foreign identity link, and matches the detail-row rule', () => {
    // The (title, artist) key is linked to a DIFFERENT track that is mid-download.
    // A resolver that trusts the identity link over the stamped extras would show
    // this row as "downloading".
    linkTrackIdentity(trackIdentityKey(OWNED_TITLE, OWNED_ARTIST), FOREIGN_ID);
    patchTrackStatus(FOREIGN_ID, { acquisitionStatus: 'pending', failureMessage: null });

    render(
      <>
        <TrackSaveControl
          testID="owned-save"
          owned={stamped}
          title={OWNED_TITLE}
          artist={OWNED_ARTIST}
          onPress={() => {}}
        />
        <DetailRowProbe />
      </>,
    );

    // Save control resolves the stamped-owned answer ("in library"), not the
    // foreign link's "downloading".
    expect(screen.getByLabelText(`${OWNED_TITLE} in library`)).toBeTruthy();
    expect(screen.queryByLabelText(`${OWNED_TITLE} downloading`)).toBeNull();

    // And it is the SAME answer the detail rows compute from the shared rule.
    expect(screen.getByText(saveControlState(stamped))).toBeTruthy();
    expect(screen.getByLabelText(saveControlLabel(saveControlState(stamped), OWNED_TITLE))).toBeTruthy();
  });
});

// The dim under a finger is the only feedback a quick-save gives before its
// glyph changes, and it must stay off a control that ignores the press — a
// control that dims without saving reads as a save that silently failed.
describe('TrackSaveControl press dim', () => {
  const DIM_TITLE = 'Nightcall';
  const DIM_ARTIST = 'Kavinsky';

  // Pressable's `pressed` comes from the touch responder, not from an onPressIn
  // prop, and fireEvent.press() grants and releases in one go — so hold the
  // responder open by hand to observe the held-down style.
  function holdDown(testID: string): void {
    fireEvent(screen.getByTestId(testID), 'responderGrant', {
      persist: () => {},
      nativeEvent: {
        touches: [],
        changedTouches: [],
        identifier: 1,
        locationX: 0,
        locationY: 0,
        pageX: 0,
        pageY: 0,
        timestamp: Date.now(),
      },
      currentTarget: { measure: () => {} },
    });
  }

  function opacityWhileHeld(testID: string): number | undefined {
    holdDown(testID);
    const style = StyleSheet.flatten(screen.getByTestId(testID).props.style) as {
      opacity?: number;
    };
    return style.opacity;
  }

  it('dims while held down when the track can still be saved', () => {
    render(
      <TrackSaveControl
        testID="dim-probe"
        owned={null}
        title={DIM_TITLE}
        artist={DIM_ARTIST}
        onPress={() => {}}
      />,
    );

    expect(opacityWhileHeld('dim-probe')).toBe(0.6);
  });

  it('stays at full opacity while held down once the track is in the library', () => {
    render(
      <TrackSaveControl
        testID="dim-probe"
        owned={{ trackId: 'owned-nightcall' as TrackId, acquisitionStatus: 'ready' }}
        title={DIM_TITLE}
        artist={DIM_ARTIST}
        onPress={() => {}}
      />,
    );

    expect(opacityWhileHeld('dim-probe')).toBeUndefined();
  });
});

// The row shares the pill's "can the user tap save" rule (#2817): a tap reaches
// onPress only to add a track or retry a failed one.
describe('TrackSaveControl tap guard', () => {
  const ROW_TITLE = 'Oblivion';
  const ROW_ARTIST = 'Grimes';

  function renderRow(owned: OwnedTrack | null): jest.Mock {
    const onPress = jest.fn();
    render(
      <TrackSaveControl
        testID="guard-row"
        owned={owned}
        title={ROW_TITLE}
        artist={ROW_ARTIST}
        onPress={onPress}
      />,
    );
    fireEvent.press(screen.getByTestId('guard-row'), pressEvent);
    return onPress;
  }

  it('saves an unsaved track when its row control is tapped', () => {
    expect(renderRow(null)).toHaveBeenCalledTimes(1);
    expect(screen.getByLabelText(`Save ${ROW_TITLE}`)).toBeTruthy();
  });

  it('retries a failed track when its row control is tapped', () => {
    const onPress = renderRow({
      trackId: 'failed-oblivion' as TrackId,
      acquisitionStatus: 'failed',
      failureMessage: 'network error',
    });

    expect(onPress).toHaveBeenCalledTimes(1);
    expect(screen.getByLabelText(`Retry saving ${ROW_TITLE}`)).toBeTruthy();
  });

  it('ignores a tap while the track is downloading', () => {
    const onPress = renderRow({ trackId: 'pending-oblivion' as TrackId, acquisitionStatus: 'pending' });

    expect(onPress).not.toHaveBeenCalled();
    expect(screen.getByLabelText(`${ROW_TITLE} downloading`)).toBeTruthy();
  });

  it('ignores a tap once the track is in the library', () => {
    const onPress = renderRow({ trackId: 'ready-oblivion' as TrackId, acquisitionStatus: 'ready' });

    expect(onPress).not.toHaveBeenCalled();
    expect(screen.getByLabelText(`${ROW_TITLE} in library`)).toBeTruthy();
  });
});
