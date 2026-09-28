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
    __http.reply('POST /v1/tracks', { status: 500 });
    const queryClient = freshClient();
    render(<QuickSaveRow />, { wrapper: createWrapper(queryClient) });

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
    __http.reply('POST /v1/discovery/events', { status: 202 });
    const queryClient = freshClient();

    const saved = render(<SaveRow title={SAVED_TITLE} artist={SAVED_ARTIST} />, {
      wrapper: createWrapper(queryClient),
    });
    await act(async () => {
      fireEvent.press(screen.getByTestId('quick-save'), pressEvent);
    });
    await waitFor(() => expect(screen.getByLabelText(`${SAVED_TITLE} in library`)).toBeTruthy());
    saved.unmount();

    render(<SaveRow title={OTHER_TITLE} artist={OTHER_ARTIST} />, {
      wrapper: createWrapper(queryClient),
    });

    expect(screen.getByLabelText(`Save ${OTHER_TITLE}`)).toBeTruthy();
    expect(screen.queryByLabelText(`${OTHER_TITLE} in library`)).toBeNull();
  });
});

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

    await waitFor(() => expect(screen.getByLabelText(`Retry saving ${TITLE}`)).toBeTruthy());
    expect(screen.queryByLabelText(`Save ${TITLE}`)).toBeNull();
  });
});

describe('TrackSaveControl stamped-owned vs identity-only agreement', () => {
  const OWNED_TITLE = 'Ivy';
  const OWNED_ARTIST = 'Frank Ocean';
  const STAMPED_ID = 'stamped-ivy' as TrackId;
  const FOREIGN_ID = 'foreign-ivy' as TrackId;

  const stamped: OwnedTrack = { trackId: STAMPED_ID, acquisitionStatus: 'ready' };
  const identity: TrackIdentity = { title: OWNED_TITLE, artist: OWNED_ARTIST };

  function DetailRowProbe(): React.ReactElement {
    const owned = useResolvedOwnedTrack(stamped, identity);
    return <Text>{saveControlState(owned)}</Text>;
  }

  it("shows the row's own stamped status, not a foreign identity link, and matches the detail-row rule", () => {
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

    expect(screen.getByLabelText(`${OWNED_TITLE} in library`)).toBeTruthy();
    expect(screen.queryByLabelText(`${OWNED_TITLE} downloading`)).toBeNull();

    expect(screen.getByText(saveControlState(stamped))).toBeTruthy();
    expect(
      screen.getByLabelText(saveControlLabel(saveControlState(stamped), OWNED_TITLE)),
    ).toBeTruthy();
  });
});

describe('TrackSaveControl press dim', () => {
  const DIM_TITLE = 'Nightcall';
  const DIM_ARTIST = 'Kavinsky';

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
    const onPress = renderRow({
      trackId: 'pending-oblivion' as TrackId,
      acquisitionStatus: 'pending',
    });

    expect(onPress).not.toHaveBeenCalled();
    expect(screen.getByLabelText(`${ROW_TITLE} downloading`)).toBeTruthy();
  });

  it('ignores a tap once the track is in the library', () => {
    const onPress = renderRow({ trackId: 'ready-oblivion' as TrackId, acquisitionStatus: 'ready' });

    expect(onPress).not.toHaveBeenCalled();
    expect(screen.getByLabelText(`${ROW_TITLE} in library`)).toBeTruthy();
  });
});
