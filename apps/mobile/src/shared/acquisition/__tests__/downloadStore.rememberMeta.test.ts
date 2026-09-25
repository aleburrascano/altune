import { asTrackId } from '@shared/api-client/ids';

import { useDownloadStore } from '../downloadStore';

// `rememberMeta` stashes a title/artist/artwork for a track that has no download
// entry yet (or ever will) — e.g. `track_added_to_library`, which always carries
// the real metadata ahead of any acquisition event. It must never surface as a
// visible entry on its own, only get merged into one once start/progress/fail
// creates it (#downloads-bar-title).
beforeEach(() => {
  useDownloadStore.getState().reset();
});

afterEach(() => {
  useDownloadStore.getState().reset();
});

describe('rememberMeta', () => {
  it('does not create a visible entry on its own', () => {
    useDownloadStore.getState().rememberMeta(asTrackId('t1'), { title: 'Voyage' });

    expect(useDownloadStore.getState().entries['t1']).toBeUndefined();
  });

  it('is picked up by a later start() with no meta of its own', () => {
    useDownloadStore
      .getState()
      .rememberMeta(asTrackId('t1'), { title: 'Voyage', artist: 'Daft Punk' });

    useDownloadStore.getState().start(asTrackId('t1'));

    expect(useDownloadStore.getState().entries['t1']).toEqual({
      trackId: 't1',
      phase: 'finding',
      title: 'Voyage',
      artist: 'Daft Punk',
      artworkUrl: null,
    });
  });

  it('is picked up by a later fail() with no prior entry (a refused save)', () => {
    useDownloadStore.getState().rememberMeta(asTrackId('t1'), { title: 'Homework' });

    useDownloadStore.getState().fail(asTrackId('t1'));

    expect(useDownloadStore.getState().entries['t1']?.title).toBe('Homework');
  });

  it('is picked up by a later complete() with no prior entry (a replayed completion)', () => {
    useDownloadStore.getState().rememberMeta(asTrackId('t1'), { title: 'Around the World' });

    useDownloadStore.getState().complete(asTrackId('t1'));

    expect(useDownloadStore.getState().entries['t1']?.title).toBe('Around the World');
    expect(useDownloadStore.getState().remembered).toEqual({});
  });

  it("loses to the event's own meta rather than overriding it", () => {
    useDownloadStore.getState().rememberMeta(asTrackId('t1'), { title: 'Remembered' });

    useDownloadStore.getState().start(asTrackId('t1'), { title: 'Explicit' });

    expect(useDownloadStore.getState().entries['t1']?.title).toBe('Explicit');
  });

  it('is cleared by reset(), so a stale value cannot leak into the next session', () => {
    useDownloadStore.getState().rememberMeta(asTrackId('t1'), { title: 'Voyage' });

    useDownloadStore.getState().reset();
    useDownloadStore.getState().start(asTrackId('t1'));

    expect(useDownloadStore.getState().entries['t1']?.title).toBeNull();
  });

  it('is cleared once consumed by start(), so it does not linger for a later fail()', () => {
    useDownloadStore.getState().rememberMeta(asTrackId('t1'), { title: 'Voyage' });
    useDownloadStore.getState().start(asTrackId('t1'));

    useDownloadStore.getState().complete(asTrackId('t1'));
    useDownloadStore.getState().remove(asTrackId('t1'));
    useDownloadStore.getState().fail(asTrackId('t1'));

    expect(useDownloadStore.getState().entries['t1']?.title).toBeNull();
  });

  it('remove() drops a remembered-only entry that never became a visible download', () => {
    useDownloadStore.getState().rememberMeta(asTrackId('t1'), { title: 'Voyage' });

    useDownloadStore.getState().remove(asTrackId('t1'));
    useDownloadStore.getState().start(asTrackId('t1'));

    expect(useDownloadStore.getState().entries['t1']?.title).toBeNull();
  });

  it("remove() for one track leaves another track's remembered meta intact", () => {
    useDownloadStore.getState().rememberMeta(asTrackId('t1'), { title: 'Voyage' });
    useDownloadStore.getState().rememberMeta(asTrackId('t2'), { title: 'Homework' });

    useDownloadStore.getState().remove(asTrackId('t1'));
    useDownloadStore.getState().start(asTrackId('t2'));

    expect(useDownloadStore.getState().entries['t2']?.title).toBe('Homework');
  });

  it('remove() of an untracked id is a no-op that leaves every remembered entry as-is', () => {
    useDownloadStore.getState().rememberMeta(asTrackId('t1'), { title: 'Voyage' });

    useDownloadStore.getState().remove(asTrackId('never-seen'));
    useDownloadStore.getState().start(asTrackId('t1'));

    expect(useDownloadStore.getState().entries['t1']?.title).toBe('Voyage');
  });

  it('remove() of an id with no entry and no remembered meta skips the state update entirely', () => {
    const before = useDownloadStore.getState();

    useDownloadStore.getState().remove(asTrackId('never-seen'));

    // Same object, not an equal-but-new one: nothing subscribed to this store
    // should re-render over a remove() that had nothing to remove.
    expect(useDownloadStore.getState()).toBe(before);
  });

  it('start() of a track with no remembered meta leaves the remembered map reference untouched', () => {
    useDownloadStore.getState().rememberMeta(asTrackId('other'), { title: 'Homework' });
    const beforeRemembered = useDownloadStore.getState().remembered;

    useDownloadStore.getState().start(asTrackId('t1'));

    expect(useDownloadStore.getState().remembered).toBe(beforeRemembered);
  });

  it("start()'s consumption of a remembered entry drops just that key, not the whole map", () => {
    useDownloadStore.getState().rememberMeta(asTrackId('t1'), { title: 'Voyage' });
    useDownloadStore.getState().rememberMeta(asTrackId('t2'), { title: 'Homework' });

    useDownloadStore.getState().start(asTrackId('t1'));

    expect(useDownloadStore.getState().remembered).toEqual({ t2: { title: 'Homework' } });
  });
});
