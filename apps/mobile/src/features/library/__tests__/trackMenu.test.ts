import { Alert } from 'react-native';

import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import { usePinnedStore, type PinnedEntry } from '@shared/offline/pinnedStore';

import { buildTrackMenuItems } from '../trackMenu';

function readyPin(trackId: string): PinnedEntry {
  return { trackId: asTrackId(trackId), status: 'ready', uri: `file:///offline-audio/${trackId}.mp3` };
}

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

type Opts = Parameters<typeof buildTrackMenuItems>[1];

let pin: jest.Mock;
let unpin: jest.Mock;
let pinnedEntries: Record<string, PinnedEntry>;

function setPinned(entries: Record<string, PinnedEntry>): void {
  pin = jest.fn();
  unpin = jest.fn();
  pinnedEntries = entries;
}

function makeOpts(over: Partial<Opts> = {}): Opts {
  return {
    offline: {
      statusOf: (trackId) => pinnedEntries[trackId]?.status,
      pin,
      unpin,
      pinMany: jest.fn(),
      unpinMany: jest.fn(),
    },
    queue: { playNext: jest.fn(), addToQueue: jest.fn() },
    onViewDetails: jest.fn(),
    danger: { label: 'Delete', onPress: jest.fn() },
    ...over,
  };
}

// The builder takes pinned state from its opts, so every test runs against an
// empty global store: a reliance on the singleton cannot pass unnoticed.
beforeEach(() => {
  usePinnedStore.setState({ entries: {} });
  setPinned({});
});

const labels = (items: { label: string }[]) => items.map((i) => i.label);

describe('buildTrackMenuItems — playback and offline items gate on the ready literal', () => {
  it('offers Play Next and Add to Queue only for a ready track', () => {
    const ready = labels(buildTrackMenuItems(makeTrack({ acquisition_status: 'ready' }), makeOpts()));
    expect(ready).toContain('Play Next');
    expect(ready).toContain('Add to Queue');
  });

  it('withholds Play Next, Add to Queue and the offline item for a pending track', () => {
    const pending = labels(
      buildTrackMenuItems(makeTrack({ acquisition_status: 'pending' }), makeOpts()),
    );
    expect(pending).not.toContain('Play Next');
    expect(pending).not.toContain('Add to Queue');
    expect(pending).not.toContain('Download');
  });

  it('always ends with View Details and the danger item', () => {
    const items = buildTrackMenuItems(makeTrack({ acquisition_status: 'pending' }), makeOpts());
    const danger = items[items.length - 1]!;
    expect(items[items.length - 2]!.label).toBe('View Details');
    expect(danger.label).toBe('Delete');
    expect(danger.tone).toBe('danger');
  });
});

describe('buildTrackMenuItems — exact composition, so a withheld arm adds nothing', () => {
  it('a ready track with every optional handler lists all actions in order', () => {
    const items = buildTrackMenuItems(
      makeTrack({ acquisition_status: 'ready' }),
      makeOpts({ onAddToPlaylist: jest.fn(), onReacquire: jest.fn() }),
    );
    expect(labels(items)).toEqual([
      'Play Next',
      'Add to Queue',
      'Add to Playlist',
      'Download',
      'Re-acquire audio',
      'View Details',
      'Delete',
    ]);
  });

  it('a ready track with no optional handlers omits playlist and re-acquire and nothing creeps in', () => {
    const items = buildTrackMenuItems(makeTrack({ acquisition_status: 'ready' }), makeOpts());
    expect(labels(items)).toEqual(['Play Next', 'Add to Queue', 'Download', 'View Details', 'Delete']);
  });

  it('a pending track offers only playlist, details and danger, with no junk from the withheld arms', () => {
    const items = buildTrackMenuItems(
      makeTrack({ acquisition_status: 'pending' }),
      makeOpts({ onAddToPlaylist: jest.fn(), onReacquire: jest.fn() }),
    );
    expect(labels(items)).toEqual(['Add to Playlist', 'View Details', 'Delete']);
  });
});

describe('buildTrackMenuItems — pressing an item performs its action on the exact track', () => {
  it('Play Next enqueues this track as the next playback track', () => {
    const opts = makeOpts();
    buildTrackMenuItems(makeTrack({ id: asTrackId('track-9'), acquisition_status: 'ready' }), opts)
      .find((i) => i.label === 'Play Next')!
      .onPress();
    expect(opts.queue.playNext).toHaveBeenCalledWith(
      expect.objectContaining({ source: { kind: 'library', trackId: 'track-9' } }),
    );
  });

  it('Add to Queue appends this track as a playback track', () => {
    const opts = makeOpts();
    buildTrackMenuItems(makeTrack({ id: asTrackId('track-9'), acquisition_status: 'ready' }), opts)
      .find((i) => i.label === 'Add to Queue')!
      .onPress();
    expect(opts.queue.addToQueue).toHaveBeenCalledWith(
      expect.objectContaining({ source: { kind: 'library', trackId: 'track-9' } }),
    );
  });

  it('Download pins the track quietly when there is room', () => {
    const alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
    pin.mockReturnValue('accepted');
    buildTrackMenuItems(makeTrack({ id: asTrackId('track-9'), acquisition_status: 'ready' }), makeOpts())
      .find((i) => i.label === 'Download')!
      .onPress();
    expect(pin).toHaveBeenCalledWith('track-9');
    expect(alert).not.toHaveBeenCalled();
    alert.mockRestore();
  });

  it('Download tells the user when the pin is refused because storage is full', () => {
    const alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
    pin.mockReturnValue('storage-full');
    buildTrackMenuItems(makeTrack({ id: asTrackId('track-9'), acquisition_status: 'ready' }), makeOpts())
      .find((i) => i.label === 'Download')!
      .onPress();
    expect(alert).toHaveBeenCalledWith('Not enough storage', expect.any(String));
    alert.mockRestore();
  });

  it('Cancel download unpins the in-flight track', () => {
    setPinned({ 'track-9': { trackId: asTrackId('track-9'), status: 'downloading' } });
    buildTrackMenuItems(makeTrack({ id: asTrackId('track-9'), acquisition_status: 'ready' }), makeOpts())
      .find((i) => i.label === 'Cancel download')!
      .onPress();
    expect(unpin).toHaveBeenCalledWith('track-9');
  });
});

describe('buildTrackMenuItems — optional actions', () => {
  it('offers Add to Playlist only when a handler is supplied, for any status', () => {
    expect(labels(buildTrackMenuItems(makeTrack(), makeOpts()))).not.toContain('Add to Playlist');
    expect(
      labels(buildTrackMenuItems(makeTrack(), makeOpts({ onAddToPlaylist: jest.fn() }))),
    ).toContain('Add to Playlist');
  });

  it('offers Re-acquire audio only when the track is ready and a handler is supplied', () => {
    expect(
      labels(
        buildTrackMenuItems(makeTrack({ acquisition_status: 'ready' }), makeOpts({ onReacquire: jest.fn() })),
      ),
    ).toContain('Re-acquire audio');
    expect(
      labels(
        buildTrackMenuItems(makeTrack({ acquisition_status: 'pending' }), makeOpts({ onReacquire: jest.fn() })),
      ),
    ).not.toContain('Re-acquire audio');
    expect(labels(buildTrackMenuItems(makeTrack({ acquisition_status: 'ready' }), makeOpts()))).not.toContain(
      'Re-acquire audio',
    );
  });
});

describe('buildTrackMenuItems — re-acquire pending state', () => {
  it('replaces Re-acquire audio with a disabled, inert pending item while the request is in flight', () => {
    const onReacquire = jest.fn();
    const items = buildTrackMenuItems(
      makeTrack({ acquisition_status: 'ready' }),
      makeOpts({ onReacquire, reacquiring: true }),
    );
    expect(labels(items)).not.toContain('Re-acquire audio');
    const pending = items.find((i) => i.label === 'Re-acquiring…')!;
    expect(pending.disabled).toBe(true);
    pending.onPress();
    expect(onReacquire).not.toHaveBeenCalled();
  });

  it('offers an enabled Re-acquire audio that fires the handler when not in flight', () => {
    const onReacquire = jest.fn();
    const item = buildTrackMenuItems(
      makeTrack({ acquisition_status: 'ready' }),
      makeOpts({ onReacquire, reacquiring: false }),
    ).find((i) => i.label === 'Re-acquire audio')!;
    expect(item.disabled).toBeUndefined();
    item.onPress();
    expect(onReacquire).toHaveBeenCalledTimes(1);
  });
});

describe('buildTrackMenuItems — the offline item reads live pinned status for a ready track', () => {
  function offlineLabel(entry: PinnedEntry | undefined): string {
    setPinned(entry ? { 'track-1': entry } : {});
    const items = buildTrackMenuItems(makeTrack({ id: asTrackId('track-1'), acquisition_status: 'ready' }), makeOpts());
    return items.find((i) => ['Download', 'Remove download', 'Cancel download', 'Retry download'].includes(i.label))!.label;
  }

  it('offers Download and pins when the track has no pinned entry', () => {
    setPinned({});
    const item = buildTrackMenuItems(makeTrack({ id: asTrackId('track-1') }), makeOpts()).find((i) => i.label === 'Download')!;
    item.onPress();
    expect(pin).toHaveBeenCalledWith('track-1');
    expect(unpin).not.toHaveBeenCalled();
  });

  it('offers Remove download and unpins when the track is already downloaded', () => {
    setPinned({ 'track-1': readyPin('track-1') });
    const item = buildTrackMenuItems(makeTrack({ id: asTrackId('track-1') }), makeOpts()).find(
      (i) => i.label === 'Remove download',
    )!;
    item.onPress();
    expect(unpin).toHaveBeenCalledWith('track-1');
    expect(pin).not.toHaveBeenCalled();
  });

  it('labels an in-flight download Cancel download and a failed one Retry download', () => {
    expect(offlineLabel({ trackId: asTrackId('track-1'), status: 'queued' })).toBe('Cancel download');
    expect(offlineLabel({ trackId: asTrackId('track-1'), status: 'downloading' })).toBe('Cancel download');
    expect(offlineLabel({ trackId: asTrackId('track-1'), status: 'failed' })).toBe('Retry download');
    expect(offlineLabel(undefined)).toBe('Download');
    expect(offlineLabel(readyPin('track-1'))).toBe('Remove download');
  });

  it('follows the pinned entries it was given when the global store disagrees', () => {
    usePinnedStore.setState({ entries: { 'track-1': readyPin('track-1') } });
    const items = buildTrackMenuItems(makeTrack({ id: asTrackId('track-1') }), makeOpts());
    expect(labels(items)).toContain('Download');
    expect(labels(items)).not.toContain('Remove download');
    items.find((i) => i.label === 'Download')!.onPress();
    expect(pin).toHaveBeenCalledWith('track-1');
    expect(unpin).not.toHaveBeenCalled();
  });

  it('retries a failed download by pinning again, not unpinning', () => {
    setPinned({ 'track-1': { trackId: asTrackId('track-1'), status: 'failed' } });
    const item = buildTrackMenuItems(makeTrack({ id: asTrackId('track-1') }), makeOpts()).find(
      (i) => i.label === 'Retry download',
    )!;
    item.onPress();
    expect(pin).toHaveBeenCalledWith('track-1');
    expect(unpin).not.toHaveBeenCalled();
  });
});
