import { Download, XCircle } from 'lucide-react-native';

import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import type { PinnedEntry } from '@shared/offline/pinnedStore';
import type { PlaybackTrack } from '@shared/playback/types';

import { buildSelectionActions } from '../ui/selectionActions';

function makeTrack(over: Partial<TrackResponse> = {}): TrackResponse {
  return {
    id: asTrackId('track-1'),
    title: 'Aerodynamic',
    artist: 'Daft Punk',
    album: 'Discovery',
    duration_seconds: 212,
    added_at: '2026-01-01T00:00:00Z',
    acquisition_status: 'ready',
    artwork_url: null,
    failure_reason: null,
    year: 2001,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
    ...over,
  };
}

function pinned(status: PinnedEntry['status']): PinnedEntry {
  return { trackId: 'x', status };
}

type Opts = Parameters<typeof buildSelectionActions>[1];

function makeOpts(over: Partial<Opts> = {}): Opts {
  return {
    pinnedEntries: {},
    pinMany: jest.fn(),
    unpin: jest.fn(),
    queue: { addToQueue: jest.fn() },
    onAddToPlaylist: jest.fn(),
    onDone: jest.fn(),
    danger: { label: 'Delete', onPress: jest.fn() },
    ...over,
  };
}

const keysOf = (selected: TrackResponse[], opts: Opts) =>
  buildSelectionActions(selected, opts).map((a) => a.key);

describe('buildSelectionActions — the four-action bar in a fixed order', () => {
  it('always offers playlist, offline, queue and danger in that order', () => {
    expect(keysOf([makeTrack()], makeOpts())).toEqual(['playlist', 'offline', 'queue', 'danger']);
  });

  it('labels the playlist and queue actions in the Track vocabulary', () => {
    const actions = buildSelectionActions([makeTrack()], makeOpts());
    expect(actions.find((a) => a.key === 'playlist')!.label).toBe('Add to Playlist');
    expect(actions.find((a) => a.key === 'queue')!.label).toBe('Add to Queue');
  });

  it('routes the playlist action straight to onAddToPlaylist', () => {
    const opts = makeOpts();
    buildSelectionActions([makeTrack()], opts).find((a) => a.key === 'playlist')!.onPress();
    expect(opts.onAddToPlaylist).toHaveBeenCalledTimes(1);
  });

  it('routes the danger action to its handler, carrying the given label and tone', () => {
    const opts = makeOpts({ danger: { label: 'Remove from library', onPress: jest.fn() } });
    const danger = buildSelectionActions([makeTrack()], opts).find((a) => a.key === 'danger')!;
    expect(danger.label).toBe('Remove from library');
    expect(danger.tone).toBe('danger');
    danger.onPress();
    expect(opts.danger.onPress).toHaveBeenCalledTimes(1);
  });
});

describe('buildSelectionActions — offline action acts on ready tracks only', () => {
  it('disables offline and queue when nothing in the selection is ready', () => {
    const actions = buildSelectionActions([makeTrack({ acquisition_status: 'pending' })], makeOpts());
    expect(actions.find((a) => a.key === 'offline')!.disabled).toBe(true);
    expect(actions.find((a) => a.key === 'queue')!.disabled).toBe(true);
  });

  it('enables offline and queue once at least one ready track is present', () => {
    const actions = buildSelectionActions([makeTrack({ acquisition_status: 'ready' })], makeOpts());
    expect(actions.find((a) => a.key === 'offline')!.disabled).toBe(false);
    expect(actions.find((a) => a.key === 'queue')!.disabled).toBe(false);
  });

  it('pins every ready track in a single pinMany call, never a per-track loop, and excludes non-ready ids', () => {
    const opts = makeOpts();
    const selected = [
      makeTrack({ id: asTrackId('r1'), acquisition_status: 'ready' }),
      makeTrack({ id: asTrackId('p1'), acquisition_status: 'pending' }),
      makeTrack({ id: asTrackId('r2'), acquisition_status: 'ready' }),
    ];
    buildSelectionActions(selected, opts).find((a) => a.key === 'offline')!.onPress();
    expect(opts.pinMany).toHaveBeenCalledTimes(1);
    expect(opts.pinMany).toHaveBeenCalledWith(['r1', 'r2']);
    expect(opts.onDone).toHaveBeenCalledTimes(1);
  });
});

describe('buildSelectionActions — offline label flips only when every ready track is already pinned', () => {
  it('shows Download with the download icon while any ready track is unpinned', () => {
    const opts = makeOpts({ pinnedEntries: { r1: pinned('ready') } });
    const offline = buildSelectionActions(
      [makeTrack({ id: asTrackId('r1'), acquisition_status: 'ready' }), makeTrack({ id: asTrackId('r2'), acquisition_status: 'ready' })],
      opts,
    ).find((a) => a.key === 'offline')!;
    expect(offline.label).toBe('Download');
    expect(offline.icon).toBe(Download);
  });

  it('shows Remove download with the remove icon and unpins each ready track when all are pinned', () => {
    const opts = makeOpts({ pinnedEntries: { r1: pinned('ready'), r2: pinned('ready') } });
    const offline = buildSelectionActions(
      [makeTrack({ id: asTrackId('r1'), acquisition_status: 'ready' }), makeTrack({ id: asTrackId('r2'), acquisition_status: 'ready' })],
      opts,
    ).find((a) => a.key === 'offline')!;
    expect(offline.label).toBe('Remove download');
    expect(offline.icon).toBe(XCircle);

    offline.onPress();
    expect(opts.unpin).toHaveBeenCalledTimes(2);
    expect(opts.unpin).toHaveBeenNthCalledWith(1, 'r1');
    expect(opts.unpin).toHaveBeenNthCalledWith(2, 'r2');
    expect(opts.pinMany).not.toHaveBeenCalled();
    expect(opts.onDone).toHaveBeenCalledTimes(1);
  });

  it('treats a pending-download entry as not-pinned, so a partly-downloaded selection still shows Download', () => {
    const opts = makeOpts({ pinnedEntries: { r1: pinned('ready'), r2: pinned('downloading') } });
    const offline = buildSelectionActions(
      [makeTrack({ id: asTrackId('r1'), acquisition_status: 'ready' }), makeTrack({ id: asTrackId('r2'), acquisition_status: 'ready' })],
      opts,
    ).find((a) => a.key === 'offline')!;
    expect(offline.label).toBe('Download');
  });

  it('does not report an all-pinned selection when there is nothing ready to pin', () => {
    const offline = buildSelectionActions(
      [makeTrack({ acquisition_status: 'pending' })],
      makeOpts(),
    ).find((a) => a.key === 'offline')!;
    expect(offline.label).toBe('Download');
  });
});

describe('buildSelectionActions — queue action', () => {
  it('adds each ready track to the queue as a playback track keyed by its id, then finishes', () => {
    const added: PlaybackTrack[] = [];
    const opts = makeOpts({ queue: { addToQueue: (t: PlaybackTrack) => added.push(t) } });
    const selected = [
      makeTrack({ id: asTrackId('r1'), acquisition_status: 'ready' }),
      makeTrack({ id: asTrackId('p1'), acquisition_status: 'pending' }),
    ];
    buildSelectionActions(selected, opts).find((a) => a.key === 'queue')!.onPress();
    expect(added).toHaveLength(1);
    expect(added[0]!.source).toEqual({ kind: 'library', trackId: 'r1' });
    expect(opts.onDone).toHaveBeenCalledTimes(1);
  });
});
