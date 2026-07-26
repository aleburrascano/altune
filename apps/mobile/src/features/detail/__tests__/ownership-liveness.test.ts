import { act, renderHook } from '@testing-library/react-native';

import {
  linkTrackIdentity,
  patchTrackStatus,
  trackIdentityKey,
  useTrackStatusStore,
} from '../../../shared/acquisition/trackStatusStore';
import { useOwnedTrack } from '../hooks/useOwnedTrack';
import { trackExtras } from '../extras-accessors';

const IDENTITY = { title: 'Get Lucky', artist: 'Daft Punk' };

beforeEach(() => {
  useTrackStatusStore.getState().reset();
});

describe('trackIdentityKey', () => {
  it('normalises case and surrounding whitespace', () => {
    expect(trackIdentityKey('  Get Lucky ', 'DAFT PUNK')).toBe(
      trackIdentityKey('get lucky', 'daft punk'),
    );
  });

  it('refuses to key on a half-known identity', () => {
    expect(trackIdentityKey('Get Lucky', '')).toBeNull();
    expect(trackIdentityKey('', 'Daft Punk')).toBeNull();
  });
});

describe('useOwnedTrack liveness', () => {
  it('returns null for a track that is neither stamped nor linked', () => {
    const { result } = renderHook(() => useOwnedTrack(trackExtras({}), IDENTITY));
    expect(result.current).toBeNull();
  });

  it('resolves ownership after the identity is linked, with no new render input', () => {
    const { result } = renderHook(() => useOwnedTrack(trackExtras({}), IDENTITY));
    expect(result.current).toBeNull();

    act(() => {
      patchTrackStatus('t-1', { acquisitionStatus: 'pending', failureMessage: null });
      linkTrackIdentity(trackIdentityKey(IDENTITY.title, IDENTITY.artist), 't-1');
    });

    expect(result.current).toEqual({ trackId: 't-1', acquisitionStatus: 'pending' });
  });

  it('follows the acquisition status through to ready', () => {
    const { result } = renderHook(() => useOwnedTrack(trackExtras({}), IDENTITY));

    act(() => {
      patchTrackStatus('t-1', { acquisitionStatus: 'pending', failureMessage: null });
      linkTrackIdentity(trackIdentityKey(IDENTITY.title, IDENTITY.artist), 't-1');
    });
    expect(result.current?.acquisitionStatus).toBe('pending');

    act(() => {
      patchTrackStatus('t-1', { acquisitionStatus: 'ready', failureMessage: null });
    });
    expect(result.current?.acquisitionStatus).toBe('ready');
  });

  it('prefers the server stamp over a linked identity', () => {
    act(() => {
      linkTrackIdentity(trackIdentityKey(IDENTITY.title, IDENTITY.artist), 'linked-id');
    });

    const extras = { track_id: 'stamped-id', acquisition_status: 'ready' };
    const { result } = renderHook(() => useOwnedTrack(trackExtras(extras), IDENTITY));

    expect(result.current?.trackId).toBe('stamped-id');
  });

  it('does not resolve a different track that shares only a title', () => {
    act(() => {
      patchTrackStatus('t-1', { acquisitionStatus: 'ready', failureMessage: null });
      linkTrackIdentity(trackIdentityKey('Get Lucky', 'Someone Else'), 't-1');
    });

    const { result } = renderHook(() => useOwnedTrack(trackExtras({}), IDENTITY));
    expect(result.current).toBeNull();
  });

  it('ignores an identity it cannot key on', () => {
    const { result } = renderHook(() =>
      useOwnedTrack(trackExtras({}), { title: 'Get Lucky', artist: null }),
    );
    expect(result.current).toBeNull();
  });
});
