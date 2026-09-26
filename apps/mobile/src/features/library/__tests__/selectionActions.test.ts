import { Alert } from 'react-native';

import { Download, XCircle } from 'lucide-react-native';

import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import type { PinnedEntry, UnpinBatchResult } from '@shared/offline/pinnedStore';
import type { PlaybackTrack } from '@shared/playback/types';

import { buildSelectionActions } from '../selectionActions';

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
  } as TrackResponse;
}

// One entry per status, each carrying only the fields its status has.
const PINNED_BY_STATUS: Record<PinnedEntry['status'], PinnedEntry> = {
  ready: { trackId: asTrackId('x'), status: 'ready', uri: 'file:///offline-audio/x.mp3' },
  failed: { trackId: asTrackId('x'), status: 'failed' },
  queued: { trackId: asTrackId('x'), status: 'queued' },
  downloading: { trackId: asTrackId('x'), status: 'downloading' },
};

function pinned(status: PinnedEntry['status']): PinnedEntry {
  return PINNED_BY_STATUS[status];
}

type Opts = Parameters<typeof buildSelectionActions>[1];
type MakeOptsInput = Partial<Omit<Opts, 'offline'>> & {
  pinnedEntries?: Record<string, PinnedEntry>;
  pinMany?: jest.Mock;
  unpinMany?: jest.Mock;
};

function makeOpts(over: MakeOptsInput = {}): Opts {
  const { pinnedEntries = {}, pinMany, unpinMany, ...rest } = over;
  return {
    offline: {
      supported: true,
      statusOf: (trackId) => pinnedEntries[trackId]?.status,
      pin: jest.fn(),
      unpin: jest.fn(),
      pinMany: pinMany ?? jest.fn().mockResolvedValue({ requested: 0, failed: 0 }),
      unpinMany: unpinMany ?? jest.fn().mockResolvedValue({ requested: 0, failed: 0 }),
    },
    queue: { addToQueueMany: jest.fn() },
    onAddToPlaylist: jest.fn(),
    onDone: jest.fn(),
    danger: { label: 'Delete', onPress: jest.fn() },
    ...rest,
  };
}

function readyTracks(count: number): TrackResponse[] {
  return Array.from({ length: count }, (_, i) => makeTrack({ id: asTrackId(`r${i}`) }));
}

function allPinned(tracks: TrackResponse[]): Record<string, PinnedEntry> {
  return Object.fromEntries(tracks.map((t) => [t.id, pinned('ready')]));
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

describe('buildSelectionActions — an empty selection acts on nothing', () => {
  it('disables every action when the selection is active but empty', () => {
    const actions = buildSelectionActions([], makeOpts());
    expect(actions.map((a) => a.disabled)).toEqual([true, true, true, true]);
  });

  it('keeps the playlist and danger actions live as soon as one track is selected', () => {
    const actions = buildSelectionActions([makeTrack({ acquisition_status: 'pending' })], makeOpts());
    expect(actions.find((a) => a.key === 'playlist')!.disabled).toBe(false);
    expect(actions.find((a) => a.key === 'danger')!.disabled).toBe(false);
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
    expect(opts.offline.pinMany).toHaveBeenCalledTimes(1);
    expect(opts.offline.pinMany).toHaveBeenCalledWith(['r1', 'r2']);
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

  it('shows Remove download with the remove icon and removes every ready track in one call when all are pinned', () => {
    const opts = makeOpts({ pinnedEntries: { r1: pinned('ready'), r2: pinned('ready') } });
    const offline = buildSelectionActions(
      [makeTrack({ id: asTrackId('r1'), acquisition_status: 'ready' }), makeTrack({ id: asTrackId('r2'), acquisition_status: 'ready' })],
      opts,
    ).find((a) => a.key === 'offline')!;
    expect(offline.label).toBe('Remove download');
    expect(offline.icon).toBe(XCircle);

    offline.onPress();
    expect(opts.offline.unpinMany).toHaveBeenCalledTimes(1);
    expect(opts.offline.unpinMany).toHaveBeenCalledWith(['r1', 'r2']);
    expect(opts.offline.pinMany).not.toHaveBeenCalled();
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
    const opts = makeOpts({
      queue: { addToQueueMany: (tracks: readonly PlaybackTrack[]) => added.push(...tracks) },
    });
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

// A "select all" over a loaded library reaches the thousands. Before #1699 both bulk
// actions ran a raw per-track forEach, so the tap fired that many store copies and
// un-awaited native/filesystem calls in one tick, and reported nothing when some failed.
describe('buildSelectionActions — a bulk action costs one call, whatever the selection size', () => {
  const SELECT_ALL_SIZE = 3_000;

  it('hands the whole ready selection to the queue in one bulk add, not one call per track', () => {
    const opts = makeOpts();
    const selected = readyTracks(SELECT_ALL_SIZE);

    buildSelectionActions(selected, opts).find((a) => a.key === 'queue')!.onPress();

    expect(opts.queue.addToQueueMany).toHaveBeenCalledTimes(1);
    expect(jest.mocked(opts.queue.addToQueueMany).mock.calls[0]?.[0]).toHaveLength(SELECT_ALL_SIZE);
  });

  it('removes the whole downloaded selection in one bulk call, not one unpin per track', () => {
    const selected = readyTracks(SELECT_ALL_SIZE);
    const opts = makeOpts({ pinnedEntries: allPinned(selected) });

    buildSelectionActions(selected, opts).find((a) => a.key === 'offline')!.onPress();

    expect(opts.offline.unpinMany).toHaveBeenCalledTimes(1);
    expect(jest.mocked(opts.offline.unpinMany).mock.calls[0]?.[0]).toHaveLength(SELECT_ALL_SIZE);
  });
});

describe('buildSelectionActions — batch download summary', () => {
  const flush = () => new Promise<void>((resolve) => setImmediate(resolve));

  beforeEach(() => {
    jest.spyOn(Alert, 'alert').mockImplementation(() => {});
  });
  afterEach(() => jest.restoreAllMocks());

  it('shows "N of M downloads failed" once a mixed batch settles', async () => {
    const opts = makeOpts({ pinMany: jest.fn().mockResolvedValue({ requested: 10, failed: 3 }) });
    buildSelectionActions(readyTracks(10), opts).find((a) => a.key === 'offline')!.onPress();
    await flush();
    expect(Alert.alert).toHaveBeenCalledTimes(1);
    expect(jest.mocked(Alert.alert).mock.calls[0]?.[1]).toContain('3 of 10 downloads failed');
  });

  it('tells the user the batch was refused when pinned storage is full', async () => {
    const opts = makeOpts({
      pinMany: jest.fn().mockResolvedValue({ requested: 0, failed: 0, refused: 'storage-full' }),
    });
    buildSelectionActions(readyTracks(3), opts).find((a) => a.key === 'offline')!.onPress();
    await flush();
    expect(Alert.alert).toHaveBeenCalledTimes(1);
    expect(jest.mocked(Alert.alert).mock.calls[0]?.[0]).toBe('Not enough storage');
  });

  it('stays quiet when every download in the batch succeeded', async () => {
    const opts = makeOpts({ pinMany: jest.fn().mockResolvedValue({ requested: 2, failed: 0 }) });
    buildSelectionActions(readyTracks(2), opts).find((a) => a.key === 'offline')!.onPress();
    await flush();
    expect(Alert.alert).not.toHaveBeenCalled();
  });
});

describe('buildSelectionActions — batch download-removal summary', () => {
  const flush = () => new Promise<void>((resolve) => setImmediate(resolve));

  beforeEach(() => {
    jest.spyOn(Alert, 'alert').mockImplementation(() => {});
  });
  afterEach(() => jest.restoreAllMocks());

  function removing(selected: TrackResponse[], result: UnpinBatchResult): Opts {
    return makeOpts({
      pinnedEntries: allPinned(selected),
      unpinMany: jest.fn().mockResolvedValue(result),
    });
  }

  it('shows "N of M downloads could not be removed" once a partial removal settles', async () => {
    const selected = readyTracks(10);
    const opts = removing(selected, { requested: 10, failed: 4 });

    buildSelectionActions(selected, opts).find((a) => a.key === 'offline')!.onPress();
    await flush();

    expect(Alert.alert).toHaveBeenCalledTimes(1);
    expect(jest.mocked(Alert.alert).mock.calls[0]?.[1]).toContain(
      '4 of 10 downloads could not be removed',
    );
  });

  it('stays quiet when every download in the selection was removed', async () => {
    const selected = readyTracks(4);
    const opts = removing(selected, { requested: 4, failed: 0 });

    buildSelectionActions(selected, opts).find((a) => a.key === 'offline')!.onPress();
    await flush();

    expect(Alert.alert).not.toHaveBeenCalled();
  });
});

describe('buildSelectionActions — offline action withheld where offline downloads are unsupported', () => {
  it('omits the offline action entirely when the platform cannot support downloads', () => {
    const opts = makeOpts({ offline: { ...makeOpts().offline, supported: false } });
    expect(keysOf([makeTrack({ acquisition_status: 'ready' })], opts)).toEqual([
      'playlist',
      'queue',
      'danger',
    ]);
  });
});
