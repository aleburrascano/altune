import { renderHook } from '@testing-library/react-native';

import {
  isTrackStatusReady,
  linkTrackIdentity,
  patchTrackStatus,
  READY_STATUS_LIMIT,
  removeTrackStatus,
  trackIdentityKey,
  useTrackIdForIdentity,
  useTrackStatus,
  useTrackStatusStore,
  type TrackStatus,
} from '../trackStatusStore';
import { asTrackId, type TrackId } from '@shared/api-client/ids';
import { toFailed, toPending, toReady, toTrackStatus } from '@shared/api-client/trackAcquisition';
import type { AcquisitionStatus } from '@shared/api-client/types';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';
import { enqueueCritical } from '@shared/telemetry/outbox';

jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));

const enqueueCriticalMock = enqueueCritical as jest.MockedFunction<typeof enqueueCritical>;

type StatusFields = { acquisitionStatus: AcquisitionStatus; failureMessage: string | null };

// Through the app's own constructors, so a pairing no transition can produce is
// one no fixture can arrange either.
function status({
  acquisitionStatus = 'pending',
  failureMessage = null,
}: Partial<StatusFields> = {}): TrackStatus {
  switch (acquisitionStatus) {
    case 'pending':
      return toTrackStatus(toPending());
    case 'ready':
      return toTrackStatus(toReady());
    case 'failed':
      return toTrackStatus(toFailed(null, failureMessage));
  }
}

beforeEach(() => {
  useTrackStatusStore.getState().reset();
});

beforeEach(() => {
  enqueueCriticalMock.mockReset().mockResolvedValue(undefined);
});

describe('sign-out', () => {
  it("drops the previous account's statuses and identity links", () => {
    patchTrackStatus(asTrackId('t-1'), status({ acquisitionStatus: 'ready' }));
    linkTrackIdentity(trackIdentityKey('Track Title', 'The Artist'), asTrackId('t-1'));

    runSignOutCleanups();

    expect(useTrackStatusStore.getState().statuses).toEqual({});
    expect(useTrackStatusStore.getState().identities).toEqual({});
    expect(useTrackStatusStore.getState().readyTrackIds).toEqual([]);
  });
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

describe('link', () => {
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

type SavedTrack = { trackId: TrackId; identity: string };

// One track of a long session, in the order the app writes it: an optimistic
// pending status, its (title, artist) identity link, then the completion event.
function completeSavedTrack(n: number): SavedTrack {
  const trackId = asTrackId(`t-${n}`);
  const identity = `identity-${n}`;
  patchTrackStatus(trackId, status());
  linkTrackIdentity(identity, trackId);
  patchTrackStatus(trackId, status({ acquisitionStatus: 'ready' }));
  return { trackId, identity };
}

function completeSavedTracks(count: number, from = 0): void {
  for (let i = from; i < from + count; i += 1) completeSavedTrack(i);
}

describe('pruning settled entries', () => {
  it('caps both maps when a session completes far more tracks than the limit', () => {
    completeSavedTracks(READY_STATUS_LIMIT * 3);

    const { statuses, identities } = useTrackStatusStore.getState();
    expect(Object.keys(statuses)).toHaveLength(READY_STATUS_LIMIT);
    expect(Object.keys(identities)).toHaveLength(READY_STATUS_LIMIT);
  });

  it('evicts the oldest completed track and its identity link, keeping the newest', () => {
    const oldest = completeSavedTrack(0);
    completeSavedTracks(READY_STATUS_LIMIT - 1, 1);
    const newest = completeSavedTrack(READY_STATUS_LIMIT);

    const { statuses, identities } = useTrackStatusStore.getState();
    expect(statuses[newest.trackId]).toEqual(status({ acquisitionStatus: 'ready' }));
    expect(identities[newest.identity]).toBe(newest.trackId);
    expect(statuses[oldest.trackId]).toBeUndefined();
    expect(identities[oldest.identity]).toBeUndefined();
  });

  it('leaves a track short of ready in place however many others complete around it', () => {
    const downloading = asTrackId('t-downloading');
    patchTrackStatus(downloading, status());
    linkTrackIdentity('identity-downloading', downloading);

    completeSavedTracks(READY_STATUS_LIMIT * 2);

    const { statuses, identities } = useTrackStatusStore.getState();
    expect(statuses[downloading]).toEqual(status());
    expect(identities['identity-downloading']).toBe(downloading);
  });

  it('spends one eviction slot on a track whose completion is replayed', () => {
    const replayed = completeSavedTrack(0);
    patchTrackStatus(replayed.trackId, status({ acquisitionStatus: 'ready' }));

    completeSavedTracks(READY_STATUS_LIMIT - 1, 1);

    expect(useTrackStatusStore.getState().statuses[replayed.trackId]).toEqual(
      status({ acquisitionStatus: 'ready' }),
    );
  });

  it('spends one eviction slot on a track removed and then restored to ready', () => {
    const restored = completeSavedTrack(0);
    removeTrackStatus(restored.trackId);
    patchTrackStatus(restored.trackId, status({ acquisitionStatus: 'ready' }));

    completeSavedTracks(READY_STATUS_LIMIT - 1, 1);

    expect(useTrackStatusStore.getState().statuses[restored.trackId]).toEqual(
      status({ acquisitionStatus: 'ready' }),
    );
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

describe('isTrackStatusReady', () => {
  it.each<[TrackStatus['acquisitionStatus'], boolean]>([
    ['ready', true],
    ['pending', false],
    ['failed', false],
  ])('a %s status reads as ready: %s', (acquisitionStatus, expected) => {
    patchTrackStatus(asTrackId('t-1'), status({ acquisitionStatus }));

    expect(isTrackStatusReady(asTrackId('t-1'))).toBe(expected);
  });

  it('reports a trackId the store never saw as not ready', () => {
    expect(isTrackStatusReady(asTrackId('t-unknown'))).toBe(false);
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

describe('patchTrackStatus — status_changed telemetry', () => {
  function lastPayload(): Record<string, unknown> {
    const call = enqueueCriticalMock.mock.calls.at(-1);
    return (call?.[0]?.payload ?? {}) as Record<string, unknown>;
  }

  it('records the transition with the given source, defaulting to response', () => {
    patchTrackStatus(asTrackId('t-1'), status({ acquisitionStatus: 'pending' }));

    expect(lastPayload()).toEqual({
      track_id: 't-1',
      action: 'status_changed',
      from: null,
      to: 'pending',
      source: 'response',
    });
  });

  it('carries the source it is given', () => {
    patchTrackStatus(asTrackId('t-1'), status({ acquisitionStatus: 'pending' }), 'optimistic');

    expect(lastPayload()['source']).toBe('optimistic');
  });

  it('does not record when the acquisition status does not change', () => {
    patchTrackStatus(asTrackId('t-1'), status({ acquisitionStatus: 'ready' }));
    enqueueCriticalMock.mockClear();

    patchTrackStatus(asTrackId('t-1'), status({ acquisitionStatus: 'ready' }));

    expect(enqueueCriticalMock).not.toHaveBeenCalled();
  });
});
