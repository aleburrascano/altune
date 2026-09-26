// Probe (#2819): AlbumTrackList is the tracklist lifted out of AlbumDetailBody,
// and it hands AlbumMoreTracks its row actions as one group. These drive it with
// a hand-built AlbumDetailState, as the body would, and pin that each list state
// keeps one "Tracks" header and that each row action reaches the right track.

import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { asTrackId } from '@shared/api-client/ids';

import type { AlbumDetailState } from '../hooks/useAlbumDetailState';
import { ownedTrack } from '../hooks/useOwnedTrack';
import { AlbumTrackList } from '../ui/AlbumTrackList';

function track(title: string): DiscoveryResult {
  return {
    kind: 'track',
    title,
    subtitle: 'Fleetwood Mac',
    image_url: null,
    confidence: 'high',
    sources: [{ provider: 'deezer', external_id: `d-${title}`, url: `https://d/${title}` }],
    extras: {},
  };
}

const DREAMS = track('Dreams');
const SONGBIRD = track('Songbird');

// The row save control stops the press reaching its row, so it needs an event.
const pressEvent = { stopPropagation: () => {} };

function albumState(overrides: Partial<AlbumDetailState> = {}): AlbumDetailState {
  return {
    tracks: [DREAMS],
    isLoading: false,
    isError: false,
    failure: null,
    refetch: jest.fn(),
    hasSources: true,
    moreExpanded: false,
    setMoreExpanded: jest.fn(),
    moreTracks: [],
    discoveryError: false,
    discoveryFailure: null,
    discoveryRefetch: jest.fn(),
    savingAll: false,
    isSavingInBatch: () => false,
    onTrackPress: jest.fn(),
    onQuickSave: jest.fn(),
    onSaveAll: jest.fn(),
    ownedFor: () => null,
    owned: { playable: [], unownedCount: 0, acquiringCount: 0 },
    playButton: { label: 'Play', disabled: true },
    onPlayOwned: jest.fn(),
    ...overrides,
  };
}

// A library album (no sources) with its "More from this album" section open.
function libraryAlbumWithMore(overrides: Partial<AlbumDetailState> = {}): AlbumDetailState {
  return albumState({
    hasSources: false,
    moreExpanded: true,
    moreTracks: [SONGBIRD],
    ...overrides,
  });
}

describe('AlbumTrackList: the Tracks header', () => {
  it.each<[string, Partial<AlbumDetailState>]>([
    ['loading', { isLoading: true, tracks: [] }],
    ['failed', { isError: true, failure: 'transient', tracks: [] }],
    ['listed', {}],
  ])('titles the list "Tracks" exactly once while %s', (_state, overrides) => {
    render(<AlbumTrackList album={albumState(overrides)} />);

    expect(screen.getAllByText(/^tracks$/i)).toHaveLength(1);
  });

  it('re-asks for the tracklist when Retry is tapped after a transient failure', () => {
    const album = albumState({ isError: true, failure: 'transient', tracks: [] });
    render(<AlbumTrackList album={album} />);

    fireEvent.press(screen.getByTestId('detail-tracklist-retry'));

    expect(album.refetch).toHaveBeenCalledTimes(1);
  });
});

describe('AlbumTrackList: row actions', () => {
  it('opens the pressed album track, not saves it', () => {
    const album = albumState();
    render(<AlbumTrackList album={album} />);

    fireEvent.press(screen.getByLabelText('Track 1: Dreams'));

    expect(album.onTrackPress).toHaveBeenCalledWith(DREAMS);
    expect(album.onQuickSave).not.toHaveBeenCalled();
  });

  it('saves the album track whose save pill is tapped, not opens it', () => {
    const album = albumState();
    render(<AlbumTrackList album={album} />);

    fireEvent.press(screen.getByLabelText('Save Dreams'), pressEvent);

    expect(album.onQuickSave).toHaveBeenCalledWith(DREAMS);
    expect(album.onTrackPress).not.toHaveBeenCalled();
  });

  it('opens the pressed "More from this album" track, not saves it', () => {
    const album = libraryAlbumWithMore();
    render(<AlbumTrackList album={album} />);

    fireEvent.press(screen.getByText('Songbird'));

    expect(album.onTrackPress).toHaveBeenCalledWith(SONGBIRD);
    expect(album.onQuickSave).not.toHaveBeenCalled();
  });

  it('saves the "More from this album" track whose save pill is tapped, not opens it', () => {
    const album = libraryAlbumWithMore();
    render(<AlbumTrackList album={album} />);

    fireEvent.press(screen.getByLabelText('Save Songbird'), pressEvent);

    expect(album.onQuickSave).toHaveBeenCalledWith(SONGBIRD);
    expect(album.onTrackPress).not.toHaveBeenCalled();
  });

  it('shows a "More from this album" track caught in a save-all run as downloading', () => {
    render(
      <AlbumTrackList
        album={libraryAlbumWithMore({ isSavingInBatch: (t) => t.title === 'Songbird' })}
      />,
    );

    expect(screen.getByLabelText('Songbird downloading')).toBeTruthy();
  });

  it('shows an owned "More from this album" track as in the library', () => {
    const owned = ownedTrack(asTrackId('trk-songbird'), 'ready', null);
    render(
      <AlbumTrackList
        album={libraryAlbumWithMore({ ownedFor: (t) => (t.title === 'Songbird' ? owned : null) })}
      />,
    );

    expect(screen.getByLabelText('Songbird in library')).toBeTruthy();
  });
});
