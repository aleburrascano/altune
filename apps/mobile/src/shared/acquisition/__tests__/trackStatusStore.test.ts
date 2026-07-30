import { renderHook } from '@testing-library/react-native';

import {
  linkTrackIdentity,
  patchTrackStatus,
  removeTrackStatus,
  trackIdentityKey,
  unlinkTrackIdentity,
  useTrackIdForIdentity,
  useTrackStatus,
  useTrackStatusStore,
  type TrackStatus,
} from '../trackStatusStore';

function status(overrides: Partial<TrackStatus> = {}): TrackStatus {
  return { acquisitionStatus: 'pending', failureMessage: null, ...overrides };
}

beforeEach(() => {
  useTrackStatusStore.getState().reset();
});

describe('patch', () => {
  it('patching a second trackId leaves the first trackId status intact', () => {
    useTrackStatusStore.getState().patch('t-1', status({ acquisitionStatus: 'pending' }));
    useTrackStatusStore.getState().patch('t-2', status({ acquisitionStatus: 'ready' }));

    expect(useTrackStatusStore.getState().statuses).toEqual({
      't-1': status({ acquisitionStatus: 'pending' }),
      't-2': status({ acquisitionStatus: 'ready' }),
    });
  });

  it('patching two distinct trackIds in the reverse order still leaves both intact', () => {
    useTrackStatusStore.getState().patch('t-2', status({ acquisitionStatus: 'ready' }));
    useTrackStatusStore.getState().patch('t-1', status({ acquisitionStatus: 'pending' }));

    expect(useTrackStatusStore.getState().statuses).toEqual({
      't-1': status({ acquisitionStatus: 'pending' }),
      't-2': status({ acquisitionStatus: 'ready' }),
    });
  });

  it('patching the same trackId twice with the same status is idempotent', () => {
    const s = status({ acquisitionStatus: 'failed', failureMessage: 'no_candidates' });

    useTrackStatusStore.getState().patch('t-1', s);
    const once = useTrackStatusStore.getState().statuses;
    useTrackStatusStore.getState().patch('t-1', s);
    const twice = useTrackStatusStore.getState().statuses;

    expect(twice).toEqual(once);
  });

  it('overwrites an existing status for the same trackId', () => {
    useTrackStatusStore.getState().patch('t-1', status({ acquisitionStatus: 'pending' }));
    useTrackStatusStore
      .getState()
      .patch('t-1', status({ acquisitionStatus: 'failed', failureMessage: 'no_source' }));

    expect(useTrackStatusStore.getState().statuses['t-1']).toEqual(
      status({ acquisitionStatus: 'failed', failureMessage: 'no_source' }),
    );
  });
});

describe('remove', () => {
  it('removes a present trackId and leaves other entries untouched', () => {
    useTrackStatusStore.getState().patch('t-1', status());
    useTrackStatusStore.getState().patch('t-2', status({ acquisitionStatus: 'ready' }));

    useTrackStatusStore.getState().remove('t-1');

    expect(useTrackStatusStore.getState().statuses).toEqual({
      't-2': status({ acquisitionStatus: 'ready' }),
    });
  });

  it('is a true no-op for a trackId that was never present', () => {
    useTrackStatusStore.getState().patch('t-2', status({ acquisitionStatus: 'ready' }));
    const before = useTrackStatusStore.getState().statuses;

    expect(() => useTrackStatusStore.getState().remove('t-absent')).not.toThrow();

    expect(useTrackStatusStore.getState().statuses).toBe(before);
  });

  it('removing the same trackId twice behaves identically to removing it once', () => {
    useTrackStatusStore.getState().patch('t-1', status());

    useTrackStatusStore.getState().remove('t-1');
    const afterFirstRemove = useTrackStatusStore.getState().statuses;
    useTrackStatusStore.getState().remove('t-1');

    expect(useTrackStatusStore.getState().statuses).toBe(afterFirstRemove);
    expect(useTrackStatusStore.getState().statuses).toEqual({});
  });
});

describe('link / unlink', () => {
  it('links an identity to a trackId', () => {
    useTrackStatusStore.getState().link('song title the artist', 't-1');

    expect(useTrackStatusStore.getState().identities['song title the artist']).toBe('t-1');
  });

  it('linking a second identity leaves the first identity mapping intact', () => {
    useTrackStatusStore.getState().link('identity-a', 't-1');
    useTrackStatusStore.getState().link('identity-b', 't-2');

    expect(useTrackStatusStore.getState().identities).toEqual({
      'identity-a': 't-1',
      'identity-b': 't-2',
    });
  });

  it('relinking the same identity to a new trackId overwrites the mapping', () => {
    useTrackStatusStore.getState().link('identity-a', 'optimistic-1');
    useTrackStatusStore.getState().link('identity-a', 'server-1');

    expect(useTrackStatusStore.getState().identities['identity-a']).toBe('server-1');
  });

  it('unlinks a present identity and leaves other identities untouched', () => {
    useTrackStatusStore.getState().link('identity-a', 't-1');
    useTrackStatusStore.getState().link('identity-b', 't-2');

    useTrackStatusStore.getState().unlink('identity-a');

    expect(useTrackStatusStore.getState().identities).toEqual({ 'identity-b': 't-2' });
  });

  it('is a true no-op when unlinking an identity that was never linked', () => {
    useTrackStatusStore.getState().link('identity-b', 't-2');
    const before = useTrackStatusStore.getState().identities;

    expect(() => useTrackStatusStore.getState().unlink('identity-absent')).not.toThrow();

    expect(useTrackStatusStore.getState().identities).toBe(before);
  });
});

describe('reset', () => {
  it('clears both statuses and identities', () => {
    useTrackStatusStore.getState().patch('t-1', status());
    useTrackStatusStore.getState().link('identity-a', 't-1');

    useTrackStatusStore.getState().reset();

    expect(useTrackStatusStore.getState().statuses).toEqual({});
    expect(useTrackStatusStore.getState().identities).toEqual({});
  });
});

describe('trackIdentityKey', () => {
  it.each<[string, string, string]>([
    ['empty title', '', 'The Artist'],
    ['empty artist', 'Song Title', ''],
    ['both empty', '', ''],
    ['whitespace-only title', '   ', 'The Artist'],
    ['whitespace-only artist', 'Song Title', '   '],
  ])('%s returns null', (_label, title, artist) => {
    expect(trackIdentityKey(title, artist)).toBeNull();
  });

  it.each<[string, string, string, string]>([
    ['normal title and artist', 'Song Title', 'The Artist', 'song title the artist'],
    ['leading/trailing whitespace', '  Song Title  ', '  The Artist  ', 'song title the artist'],
    ['differing case', 'SONG TITLE', 'the artist', 'song title the artist'],
  ])('%s normalizes with a space separator', (_label, title, artist, expected) => {
    expect(trackIdentityKey(title, artist)).toBe(expected);
  });
});

describe('linkTrackIdentity', () => {
  it('links the computed identity to the trackId in the store', () => {
    linkTrackIdentity('song title the artist', 't-1');

    expect(useTrackStatusStore.getState().identities['song title the artist']).toBe('t-1');
  });

  it('is a no-op when identity is null', () => {
    linkTrackIdentity(null, 't-1');

    expect(useTrackStatusStore.getState().identities).toEqual({});
  });
});

describe('unlinkTrackIdentity', () => {
  it('removes the identity from the store', () => {
    useTrackStatusStore.getState().link('song title the artist', 't-1');

    unlinkTrackIdentity('song title the artist');

    expect(useTrackStatusStore.getState().identities).toEqual({});
  });

  it('is a no-op when identity is null', () => {
    useTrackStatusStore.getState().link('song title the artist', 't-1');

    unlinkTrackIdentity(null);

    expect(useTrackStatusStore.getState().identities).toEqual({ 'song title the artist': 't-1' });
  });
});

describe('useTrackIdForIdentity', () => {
  it('resolves the trackId linked to an identity', () => {
    useTrackStatusStore.getState().link('song title the artist', 't-1');

    const { result } = renderHook(() => useTrackIdForIdentity('song title the artist'));

    expect(result.current).toBe('t-1');
  });

  it('returns undefined when identity is null', () => {
    const { result } = renderHook(() => useTrackIdForIdentity(null));

    expect(result.current).toBeUndefined();
  });
});

describe('useTrackStatus', () => {
  it('resolves the status for a trackId', () => {
    useTrackStatusStore.getState().patch('t-1', status({ acquisitionStatus: 'ready' }));

    const { result } = renderHook(() => useTrackStatus('t-1'));

    expect(result.current).toEqual(status({ acquisitionStatus: 'ready' }));
  });

  it('returns undefined when trackId is null', () => {
    const { result } = renderHook(() => useTrackStatus(null));

    expect(result.current).toBeUndefined();
  });
});

describe('patchTrackStatus / removeTrackStatus', () => {
  it('patchTrackStatus writes through to the store', () => {
    patchTrackStatus('t-1', status({ acquisitionStatus: 'ready' }));

    expect(useTrackStatusStore.getState().statuses['t-1']).toEqual(
      status({ acquisitionStatus: 'ready' }),
    );
  });

  it('removeTrackStatus deletes through to the store', () => {
    patchTrackStatus('t-1', status());

    removeTrackStatus('t-1');

    expect(useTrackStatusStore.getState().statuses['t-1']).toBeUndefined();
  });
});
