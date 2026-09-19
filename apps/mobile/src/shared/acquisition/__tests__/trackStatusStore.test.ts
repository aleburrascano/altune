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
import { asTrackId } from '@shared/api-client/ids';

function status(overrides: Partial<TrackStatus> = {}): TrackStatus {
  return { acquisitionStatus: 'pending', failureMessage: null, ...overrides };
}

beforeEach(() => {
  useTrackStatusStore.getState().reset();
});

describe('patch', () => {
  it('patching a second trackId leaves the first trackId status intact', () => {
    useTrackStatusStore.getState().patch(asTrackId('t-1'), status({ acquisitionStatus: 'pending' }));
    useTrackStatusStore.getState().patch(asTrackId('t-2'), status({ acquisitionStatus: 'ready' }));

    expect(useTrackStatusStore.getState().statuses).toEqual({
      't-1': status({ acquisitionStatus: 'pending' }),
      't-2': status({ acquisitionStatus: 'ready' }),
    });
  });

  it('patching two distinct trackIds in the reverse order still leaves both intact', () => {
    useTrackStatusStore.getState().patch(asTrackId('t-2'), status({ acquisitionStatus: 'ready' }));
    useTrackStatusStore.getState().patch(asTrackId('t-1'), status({ acquisitionStatus: 'pending' }));

    expect(useTrackStatusStore.getState().statuses).toEqual({
      't-1': status({ acquisitionStatus: 'pending' }),
      't-2': status({ acquisitionStatus: 'ready' }),
    });
  });

  it('patching the same trackId twice with the same status is idempotent', () => {
    const s = status({ acquisitionStatus: 'failed', failureMessage: 'no_candidates' });

    useTrackStatusStore.getState().patch(asTrackId('t-1'), s);
    const once = useTrackStatusStore.getState().statuses;
    useTrackStatusStore.getState().patch(asTrackId('t-1'), s);
    const twice = useTrackStatusStore.getState().statuses;

    expect(twice).toEqual(once);
  });

  it('overwrites an existing status for the same trackId', () => {
    useTrackStatusStore.getState().patch(asTrackId('t-1'), status({ acquisitionStatus: 'pending' }));
    useTrackStatusStore
      .getState()
      .patch(asTrackId('t-1'), status({ acquisitionStatus: 'failed', failureMessage: 'no_source' }));

    expect(useTrackStatusStore.getState().statuses['t-1']).toEqual(
      status({ acquisitionStatus: 'failed', failureMessage: 'no_source' }),
    );
  });
});

describe('remove', () => {
  it('removes a present trackId and leaves other entries untouched', () => {
    useTrackStatusStore.getState().patch(asTrackId('t-1'), status());
    useTrackStatusStore.getState().patch(asTrackId('t-2'), status({ acquisitionStatus: 'ready' }));

    useTrackStatusStore.getState().remove(asTrackId('t-1'));

    expect(useTrackStatusStore.getState().statuses).toEqual({
      't-2': status({ acquisitionStatus: 'ready' }),
    });
  });

  it('is a true no-op for a trackId that was never present', () => {
    useTrackStatusStore.getState().patch(asTrackId('t-2'), status({ acquisitionStatus: 'ready' }));
    const before = useTrackStatusStore.getState().statuses;

    expect(() => useTrackStatusStore.getState().remove(asTrackId('t-absent'))).not.toThrow();

    expect(useTrackStatusStore.getState().statuses).toBe(before);
  });

  it('removing the same trackId twice behaves identically to removing it once', () => {
    useTrackStatusStore.getState().patch(asTrackId('t-1'), status());

    useTrackStatusStore.getState().remove(asTrackId('t-1'));
    const afterFirstRemove = useTrackStatusStore.getState().statuses;
    useTrackStatusStore.getState().remove(asTrackId('t-1'));

    expect(useTrackStatusStore.getState().statuses).toBe(afterFirstRemove);
    expect(useTrackStatusStore.getState().statuses).toEqual({});
  });
});

describe('link / unlink', () => {
  it('links an identity to a trackId', () => {
    useTrackStatusStore.getState().link('song title the artist', asTrackId('t-1'));

    expect(useTrackStatusStore.getState().identities['song title the artist']).toBe('t-1');
  });

  it('linking a second identity leaves the first identity mapping intact', () => {
    useTrackStatusStore.getState().link('identity-a', asTrackId('t-1'));
    useTrackStatusStore.getState().link('identity-b', asTrackId('t-2'));

    expect(useTrackStatusStore.getState().identities).toEqual({
      'identity-a': 't-1',
      'identity-b': 't-2',
    });
  });

  it('relinking the same identity to a new trackId overwrites the mapping', () => {
    useTrackStatusStore.getState().link('identity-a', asTrackId('optimistic-1'));
    useTrackStatusStore.getState().link('identity-a', asTrackId('server-1'));

    expect(useTrackStatusStore.getState().identities['identity-a']).toBe('server-1');
  });

  it('unlinks a present identity and leaves other identities untouched', () => {
    useTrackStatusStore.getState().link('identity-a', asTrackId('t-1'));
    useTrackStatusStore.getState().link('identity-b', asTrackId('t-2'));

    useTrackStatusStore.getState().unlink('identity-a');

    expect(useTrackStatusStore.getState().identities).toEqual({ 'identity-b': 't-2' });
  });

  it('is a true no-op when unlinking an identity that was never linked', () => {
    useTrackStatusStore.getState().link('identity-b', asTrackId('t-2'));
    const before = useTrackStatusStore.getState().identities;

    expect(() => useTrackStatusStore.getState().unlink('identity-absent')).not.toThrow();

    expect(useTrackStatusStore.getState().identities).toBe(before);
  });
});

describe('reset', () => {
  it('clears both statuses and identities', () => {
    useTrackStatusStore.getState().patch(asTrackId('t-1'), status());
    useTrackStatusStore.getState().link('identity-a', asTrackId('t-1'));

    useTrackStatusStore.getState().reset();

    expect(useTrackStatusStore.getState().statuses).toEqual({});
    expect(useTrackStatusStore.getState().identities).toEqual({});
  });
});

function foldingBy(locale: string): (this: string, locales?: Intl.LocalesArgument) => string {
  const namedLocaleFold = String.prototype.toLocaleLowerCase;
  return function (this: string, locales?: Intl.LocalesArgument): string {
    return namedLocaleFold.call(this, locales ?? locale);
  };
}

// Stands in for a device whose engine folds by the host locale whenever no locale
// is named — the Turkish `İ`/`I` case (#1778). Only a fold that names its own
// locale reads the same here as it does on any other device.
function onDeviceWithLocale<T>(locale: string, read: () => T): T {
  const fold = foldingBy(locale);
  const unnamedFold = jest.spyOn(String.prototype, 'toLowerCase').mockImplementation(fold);
  const namedFold = jest.spyOn(String.prototype, 'toLocaleLowerCase').mockImplementation(fold);
  try {
    return read();
  } finally {
    unnamedFold.mockRestore();
    namedFold.mockRestore();
  }
}

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
    ['normal title and artist', 'Song Title', 'The Artist', '10:song title:the artist'],
    ['leading/trailing whitespace', '  Song Title  ', '  The Artist  ', '10:song title:the artist'],
    ['differing case', 'SONG TITLE', 'the artist', '10:song title:the artist'],
  ])('%s normalizes with a length-prefixed key', (_label, title, artist, expected) => {
    expect(trackIdentityKey(title, artist)).toBe(expected);
  });

  it('does not collide when distinct (title, artist) pairs share a space-joined string', () => {
    // Both pairs canonicalize to "encore jay z interlude" under a plain-space
    // join, but they are different tracks and must not share a key.
    const a = trackIdentityKey('Encore', 'Jay Z Interlude');
    const b = trackIdentityKey('Encore Jay Z', 'Interlude');

    expect(a).not.toBeNull();
    expect(b).not.toBeNull();
    expect(a).not.toBe(b);
  });

  it('folds a title carrying İ and I to the same key whatever the device locale is', () => {
    const onTurkishDevice = onDeviceWithLocale('tr-TR', () =>
      trackIdentityKey('İyi Işık', 'Sanatçı'),
    );
    const onUsDevice = onDeviceWithLocale('en-US', () => trackIdentityKey('İyi Işık', 'Sanatçı'));

    expect(onTurkishDevice).not.toBeNull();
    expect(onTurkishDevice).toBe(onUsDevice);
  });
});

describe('linkTrackIdentity', () => {
  it('links the computed identity to the trackId in the store', () => {
    linkTrackIdentity('song title the artist', asTrackId('t-1'));

    expect(useTrackStatusStore.getState().identities['song title the artist']).toBe('t-1');
  });

  it('is a no-op when identity is null', () => {
    linkTrackIdentity(null, asTrackId('t-1'));

    expect(useTrackStatusStore.getState().identities).toEqual({});
  });
});

describe('unlinkTrackIdentity', () => {
  it('removes the identity from the store', () => {
    useTrackStatusStore.getState().link('song title the artist', asTrackId('t-1'));

    unlinkTrackIdentity('song title the artist');

    expect(useTrackStatusStore.getState().identities).toEqual({});
  });

  it('is a no-op when identity is null', () => {
    useTrackStatusStore.getState().link('song title the artist', asTrackId('t-1'));

    unlinkTrackIdentity(null);

    expect(useTrackStatusStore.getState().identities).toEqual({ 'song title the artist': 't-1' });
  });
});

describe('useTrackIdForIdentity', () => {
  it('resolves the trackId linked to an identity', () => {
    useTrackStatusStore.getState().link('song title the artist', asTrackId('t-1'));

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
    useTrackStatusStore.getState().patch(asTrackId('t-1'), status({ acquisitionStatus: 'ready' }));

    const { result } = renderHook(() => useTrackStatus(asTrackId('t-1')));

    expect(result.current).toEqual(status({ acquisitionStatus: 'ready' }));
  });

  it('returns undefined when trackId is null', () => {
    const { result } = renderHook(() => useTrackStatus(null));

    expect(result.current).toBeUndefined();
  });
});

describe('patchTrackStatus / removeTrackStatus', () => {
  it('patchTrackStatus writes through to the store', () => {
    patchTrackStatus(asTrackId('t-1'), status({ acquisitionStatus: 'ready' }));

    expect(useTrackStatusStore.getState().statuses['t-1']).toEqual(
      status({ acquisitionStatus: 'ready' }),
    );
  });

  it('removeTrackStatus deletes through to the store', () => {
    patchTrackStatus(asTrackId('t-1'), status());

    removeTrackStatus(asTrackId('t-1'));

    expect(useTrackStatusStore.getState().statuses['t-1']).toBeUndefined();
  });
});

describe('track id branding', () => {
  // Compile-time guards: tsc fails if the store starts accepting a bare string as a track id
  // again, which is what let an identity key and a track id be swapped silently.
  it('refuses a bare string where a TrackId belongs', () => {
    const identity = trackIdentityKey('Song Title', 'The Artist') ?? '';

    // @ts-expect-error the identity key is not a TrackId, so swapped arguments do not compile
    linkTrackIdentity(asTrackId('t-1'), identity);
    // @ts-expect-error a raw string must go through asTrackId / parseTrackId first
    patchTrackStatus('t-1', status());

    expect(useTrackStatusStore.getState().identities).toEqual({ 't-1': identity });
  });
});
