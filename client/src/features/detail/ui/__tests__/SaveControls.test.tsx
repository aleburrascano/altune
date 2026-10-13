import React from 'react';
import { Text, StyleSheet } from 'react-native';

import { fireEvent, render, screen, waitFor, act, within } from '@testing-library/react-native';
import type { CreateTrackRequest } from '@shared/apiClient/trackResponse';
import type { TrackId } from '@shared/apiClient/ids';
import {
  linkTrackIdentity,
  patchTrackStatus,
  trackIdentityKey,
  useTrackStatusStore,
} from '@shared/acquisition/trackStatusStore';
import type { DiscoveryResult } from '@shared/apiClient/discovery';

import {
  SaveAllPill,
  SavePillShell,
  type SavePillShellProps,
  TrackSaveControl,
  TrackSavePill,
} from '@features/detail/ui/SaveControls';
import {
  useOwnedPlayback,
  type OwnedPlaybackContext,
} from '@features/detail/hooks/useOwnedPlayback';
import { useSaveTrack } from '@features/detail/hooks/useSaveTrack';
import { useResolvedOwnedTrack, type TrackIdentity } from '@features/detail/hooks/useOwnedTrack';
import type { OwnedTrack } from '@features/detail/domain/ownedTrack';
import {
  saveControlLabel,
  saveControlState,
  type SaveState,
} from '@features/detail/domain/saveTrack';
import {
  createTestQueryClient,
  createPlaybackWrapper,
  signInTestUser,
} from '@testing/queryHarness';

const { __http } = require('@testing/doubles/fetch.js');

describe('SaveAllPill', () => {
  function renderPill(unownedCount: number, saving: boolean): jest.Mock {
    const onSave = jest.fn();
    render(<SaveAllPill unownedCount={unownedCount} saving={saving} onSave={onSave} />);
    return onSave;
  }

  describe('SaveAllPill', () => {
    it('starts a save-all run when tapped with unowned tracks', () => {
      const onSave = renderPill(2, false);

      fireEvent.press(screen.getByTestId('detail-save-all'));

      expect(onSave).toHaveBeenCalledTimes(1);
    });

    it('ignores taps while a save-all run is in flight', () => {
      const onSave = renderPill(2, true);

      fireEvent.press(screen.getByTestId('detail-save-all'));
      fireEvent.press(screen.getByTestId('detail-save-all'));

      expect(onSave).not.toHaveBeenCalled();
    });

    it('announces disabled but never busy while saving', () => {
      renderPill(2, true);

      const state = screen.getByTestId('detail-save-all').props.accessibilityState;
      expect(state?.disabled).toBe(true);
      expect(state?.busy).not.toBe(true);
    });

    it('renders nothing when every track is already owned', () => {
      renderPill(0, false);

      expect(screen.queryByTestId('detail-save-all')).toBeNull();
    });
  });
});

describe('SavePillShell', () => {
  function renderShell(overrides: Partial<SavePillShellProps> = {}): jest.Mock {
    const onPress = jest.fn();
    render(
      <SavePillShell
        onPress={onPress}
        disabled={false}
        interactive
        accessibilityLabel="Save everything"
        testID="shell"
        {...overrides}
      >
        <Text>Save 4</Text>
      </SavePillShell>,
    );
    return onPress;
  }

  describe('SavePillShell', () => {
    it('exposes the caller label, testID and content', () => {
      renderShell();

      expect(screen.getByLabelText('Save everything')).toBeTruthy();
      expect(screen.getByTestId('shell')).toBeTruthy();
      expect(screen.getByText('Save 4')).toBeTruthy();
    });

    it('announces exactly the accessibility state the caller passes', () => {
      renderShell({ disabled: true, accessibilityState: { disabled: true, busy: true } });

      const state = screen.getByTestId('shell').props.accessibilityState;
      expect(state?.disabled).toBe(true);
      expect(state?.busy).toBe(true);
    });

    it('calls onPress when tapped while enabled and interactive', () => {
      const onPress = renderShell();

      fireEvent.press(screen.getByTestId('shell'));

      expect(onPress).toHaveBeenCalledTimes(1);
    });

    it('ignores taps while disabled', () => {
      const onPress = renderShell({ disabled: true, accessibilityState: { disabled: true } });

      fireEvent.press(screen.getByTestId('shell'));

      expect(onPress).not.toHaveBeenCalled();
    });
  });
});

describe('TrackSaveControl', () => {
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

  const quickSaveContext: OwnedPlaybackContext = {
    title: null,
    image: null,
    enrich: (track) => track,
    retryEntryPoint: 'album_row',
  };

  function quickSaveTrack(): DiscoveryResult {
    return {
      kind: 'track',
      title: TITLE,
      subtitle: ARTIST,
      image_url: null,
      confidence: 'high',
      sources: [],
      extras: {},
    };
  }

  function QuickSaveRow(): React.ReactElement {
    const save = useSaveTrack();
    const { onQuickSave } = useOwnedPlayback([], quickSaveContext, save);
    return (
      <TrackSaveControl
        testID="quick-save"
        owned={null}
        title={TITLE}
        artist={ARTIST}
        onPress={() => onQuickSave(quickSaveTrack())}
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

  beforeEach(async () => {
    await signInTestUser();
    __http.replyAll({ status: 204 });
    useTrackStatusStore.getState().reset();
  });

  afterEach(() => {
    useTrackStatusStore.getState().reset();
  });

  describe('TrackSaveControl propagation', () => {
    it('stops the press from reaching the enclosing row', () => {
      const stopPropagation = jest.fn();
      const onPress = jest.fn();
      render(
        <TrackSaveControl
          testID="quick-save"
          owned={null}
          title={TITLE}
          artist={ARTIST}
          onPress={onPress}
        />,
        { wrapper: createPlaybackWrapper(createTestQueryClient()) },
      );
      fireEvent.press(screen.getByTestId('quick-save'), { stopPropagation });
      expect(stopPropagation).toHaveBeenCalledTimes(1);
      expect(onPress).toHaveBeenCalledTimes(1);
    });
  });

  describe('TrackSaveControl quick-save re-entrancy', () => {
    it('fires a single createTrack POST when the save glyph is double-tapped before the mutation settles', async () => {
      __http.reply('POST /v1/tracks', { status: 500 });
      const queryClient = createTestQueryClient({ mutations: true });
      render(<QuickSaveRow />, { wrapper: createPlaybackWrapper(queryClient) });

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
      const queryClient = createTestQueryClient({ mutations: true });

      const saved = render(<SaveRow title={SAVED_TITLE} artist={SAVED_ARTIST} />, {
        wrapper: createPlaybackWrapper(queryClient),
      });
      await act(async () => {
        fireEvent.press(screen.getByTestId('quick-save'), pressEvent);
      });
      await waitFor(() => expect(screen.getByLabelText(`${SAVED_TITLE} in library`)).toBeTruthy());
      saved.unmount();

      render(<SaveRow title={OTHER_TITLE} artist={OTHER_ARTIST} />, {
        wrapper: createPlaybackWrapper(queryClient),
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
      render(<ClaimedRow />, {
        wrapper: createPlaybackWrapper(createTestQueryClient({ mutations: true })),
      });

      await act(async () => {
        fireEvent.press(screen.getByTestId('quick-save'), pressEvent);
      });

      expect(__http.countFor('POST /v1/tracks')).toBe(0);
    });

    it('reads as already saving rather than as an untouched track', () => {
      render(<ClaimedRow />, {
        wrapper: createPlaybackWrapper(createTestQueryClient({ mutations: true })),
      });

      expect(screen.getByLabelText(`${TITLE} downloading`)).toBeTruthy();
      expect(screen.queryByLabelText(`Save ${TITLE}`)).toBeNull();
    });
  });

  describe('TrackSaveControl quick-save failure', () => {
    it('surfaces a retry/failure state on the row when the save mutation fails', async () => {
      __http.fail('POST /v1/tracks');
      const queryClient = createTestQueryClient({ mutations: true });
      render(<QuickSaveRow />, { wrapper: createPlaybackWrapper(queryClient) });

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
        retryable: true,
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
      const onPress = renderRow({
        trackId: 'ready-oblivion' as TrackId,
        acquisitionStatus: 'ready',
      });

      expect(onPress).not.toHaveBeenCalled();
      expect(screen.getByLabelText(`${ROW_TITLE} in library`)).toBeTruthy();
    });
  });

  describe('TrackSaveControl after a fast-failing retry', () => {
    it('lets a failed track be pressed again once a retry patched pending then failed in one act', () => {
      const trackId = 'failed-again' as TrackId;
      const failed = {
        acquisitionStatus: 'failed' as const,
        failureMessage: 'boom',
        retryable: true,
      };
      patchTrackStatus(trackId, failed);
      const onPress = jest.fn();
      render(
        <TrackSaveControl
          testID="retry-row"
          owned={{ trackId, ...failed }}
          title={TITLE}
          artist={ARTIST}
          onPress={onPress}
        />,
      );

      fireEvent.press(screen.getByTestId('retry-row'), pressEvent);
      act(() => {
        patchTrackStatus(trackId, { acquisitionStatus: 'pending', failureMessage: null });
        patchTrackStatus(trackId, failed);
      });
      fireEvent.press(screen.getByTestId('retry-row'), pressEvent);

      expect(onPress).toHaveBeenCalledTimes(2);
    });
  });
});

describe('TrackSavePill', () => {
  const TITLE = 'Midnight City';

  function renderPill(save: SaveState): jest.Mock {
    const onSave = jest.fn();
    render(<TrackSavePill save={save} onSave={onSave} title={TITLE} />);
    return onSave;
  }

  function pill(): ReturnType<typeof screen.getByRole> {
    return screen.getByRole('button');
  }

  function pillIsDisabled(): boolean {
    return pill().props.accessibilityState?.disabled === true;
  }

  describe('TrackSavePill labels', () => {
    it('offers to save an unsaved track', () => {
      renderPill('add');

      expect(screen.getByLabelText(`Save ${TITLE}`)).toBeTruthy();
      expect(within(pill()).getByText('Save')).toBeTruthy();
      expect(pillIsDisabled()).toBe(false);
    });

    it('shows the plain save affordance, disabled, for a track with no known artist', () => {
      renderPill('disabled');

      expect(screen.getByLabelText(`Save ${TITLE}`)).toBeTruthy();
      expect(within(pill()).getByText('Save')).toBeTruthy();
      expect(pillIsDisabled()).toBe(true);
    });

    it('announces a download in progress while saving', () => {
      renderPill('saving');

      expect(screen.getByLabelText(`${TITLE} downloading`)).toBeTruthy();
      expect(within(pill()).getByText('Saving…')).toBeTruthy();
      expect(pillIsDisabled()).toBe(true);
    });

    it('announces library membership once saved', () => {
      renderPill('ready');

      expect(screen.getByLabelText(`${TITLE} in library`)).toBeTruthy();
      expect(within(pill()).getByText('Saved')).toBeTruthy();
      expect(pillIsDisabled()).toBe(true);
    });

    it('offers a retry after a failed save', () => {
      renderPill('failed');

      expect(screen.getByLabelText(`Retry saving ${TITLE}`)).toBeTruthy();
      expect(within(pill()).getByText('Retry')).toBeTruthy();
      expect(pillIsDisabled()).toBe(false);
    });

    it('names a permanent refusal and offers no retry', () => {
      renderPill('rejected');

      expect(screen.getByLabelText(`Couldn't save ${TITLE}`)).toBeTruthy();
      expect(within(pill()).getByText("Can't save")).toBeTruthy();
      expect(pillIsDisabled()).toBe(true);
    });
  });

  describe('TrackSavePill tap guard', () => {
    it('saves when tapped on an unsaved track', () => {
      const onSave = renderPill('add');

      fireEvent.press(pill());

      expect(onSave).toHaveBeenCalledTimes(1);
    });

    it('retries when tapped after a failed save', () => {
      const onSave = renderPill('failed');

      fireEvent.press(pill());

      expect(onSave).toHaveBeenCalledTimes(1);
    });

    it.each<SaveState>(['disabled', 'saving', 'ready', 'rejected'])(
      'ignores a tap while %s',
      (save) => {
        const onSave = renderPill(save);

        fireEvent.press(pill());
        fireEvent.press(pill());

        expect(onSave).not.toHaveBeenCalled();
      },
    );
  });
});
