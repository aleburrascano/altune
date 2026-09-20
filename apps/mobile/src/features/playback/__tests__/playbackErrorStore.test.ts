import { act, renderHook } from '@testing-library/react-native';

import { ApiError, NetworkError } from '@shared/errors';
import { asTrackId } from '@shared/api-client/ids';
import { trackKey } from '@shared/playback/trackKey';

import { classifyNativePlaybackError, classifyPlaybackFailure } from '../classifyPlaybackError';
import {
  clearPlaybackError,
  reportPlaybackError,
  usePlaybackErrorFor,
  usePlaybackErrorStore,
} from '../playbackErrorStore';
import { canRetryPlaybackError } from '../retryPolicy';

import { libraryTrack } from './fixtures';

const FAILED_KEY = trackKey(libraryTrack());
const OTHER_KEY = trackKey(
  libraryTrack({ source: { kind: 'library', trackId: asTrackId('trk-2') } }),
);

afterEach(() => {
  usePlaybackErrorStore.getState().clear();
});

describe('playbackErrorStore — recording and clearing a track error', () => {
  it('records the failing key, its kind and its message', () => {
    reportPlaybackError(FAILED_KEY, 'network', 'Could not load this track');

    expect(usePlaybackErrorStore.getState().key).toBe(FAILED_KEY);
    expect(usePlaybackErrorStore.getState().kind).toBe('network');
    expect(usePlaybackErrorStore.getState().message).toBe('Could not load this track');
  });

  it('clears the key, the kind and the message', () => {
    reportPlaybackError(FAILED_KEY, 'unknown', 'Could not load this track');

    clearPlaybackError();

    expect(usePlaybackErrorStore.getState().key).toBeNull();
    expect(usePlaybackErrorStore.getState().kind).toBeNull();
    expect(usePlaybackErrorStore.getState().message).toBeNull();
  });
});

describe('usePlaybackErrorFor — the failure a given track should show', () => {
  it('returns the message when the reported key matches', () => {
    reportPlaybackError(FAILED_KEY, 'unknown', 'Could not load this track');

    const { result } = renderHook(() => usePlaybackErrorFor(FAILED_KEY));

    expect(result.current?.message).toBe('Could not load this track');
  });

  it('returns the kind, so one message can be shown for two different failures', () => {
    reportPlaybackError(FAILED_KEY, 'not_found', 'Could not load this track');

    const { result } = renderHook(() => usePlaybackErrorFor(FAILED_KEY));
    const gone = result.current;

    act(() => reportPlaybackError(FAILED_KEY, 'network', 'Could not load this track'));

    expect(gone?.kind).toBe('not_found');
    expect(result.current?.kind).toBe('network');
    expect(result.current?.message).toBe(gone?.message);
  });

  it('keeps the same reference while the stored failure does not change', () => {
    reportPlaybackError(FAILED_KEY, 'network', 'Could not load this track');

    const { result, rerender } = renderHook(() => usePlaybackErrorFor(FAILED_KEY));
    const first = result.current;
    rerender(undefined);

    expect(result.current).toBe(first);
  });

  it('returns null for a different track than the one that failed', () => {
    reportPlaybackError(FAILED_KEY, 'unknown', 'Could not load this track');

    const { result } = renderHook(() => usePlaybackErrorFor(OTHER_KEY));

    expect(result.current).toBeNull();
  });

  it('returns null when the queried key is null', () => {
    reportPlaybackError(FAILED_KEY, 'unknown', 'Could not load this track');

    const { result } = renderHook(() => usePlaybackErrorFor(null));

    expect(result.current).toBeNull();
  });
});

describe('reportPlaybackError — secrets in a native error message are redacted before storing', () => {
  const SIGNED_URL =
    'https://audio.altune.example/tracks/trk-1.m4a?X-Amz-Credential=AKIAEXAMPLE&X-Amz-Signature=deadbeefcafe&token=s3cr3t-token';

  function storedMessage(message: string): string | null {
    reportPlaybackError(FAILED_KEY, 'unknown', message);
    return usePlaybackErrorStore.getState().message;
  }

  it('redacts a whole signed URL, including its query-string token, in what the UI reads', () => {
    reportPlaybackError(
      FAILED_KEY,
      'unknown',
      `Source error: Response code: 403 for ${SIGNED_URL}`,
    );

    const { result } = renderHook(() => usePlaybackErrorFor(FAILED_KEY));

    expect(result.current?.message).toBe('Source error: Response code: 403 for [redacted url]');
  });

  it('redacts a bearer token and a raw JWT', () => {
    const jwt = 'eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyIn0.c2lnbmF0dXJl';

    const message = storedMessage(`request failed (Authorization: Bearer ${jwt}) jwt ${jwt}`);

    expect(message).not.toContain('eyJ');
    expect(message).not.toContain('c2lnbmF0dXJl');
  });

  it('redacts a scheme-less query string and named secret fields', () => {
    const message = storedMessage(
      'cannot open audio.altune.example/t.m4a?sig=abc123&exp=99 (signature: zzz999, api_key=kkk)',
    );

    expect(message).not.toMatch(/abc123|zzz999|kkk/);
    expect(message).toContain('audio.altune.example/t.m4a?[redacted]');
  });

  it('redacts through the store action itself, not only the helper', () => {
    usePlaybackErrorStore.getState().report(FAILED_KEY, 'unknown', `failed ${SIGNED_URL}`);

    expect(usePlaybackErrorStore.getState().message).toBe('failed [redacted url]');
  });

  it('leaves an ordinary message untouched', () => {
    expect(storedMessage('Could not load this track')).toBe('Could not load this track');
  });

  it('caps an oversized native message so redaction stays cheap', () => {
    const message = storedMessage(`token=abc ${'a'.repeat(200_000)}`);

    expect(message?.length).toBeLessThan(600);
    expect(message).not.toContain('abc');
  });
});

describe('playback error key branding', () => {
  // Compile-time guards: tsc fails if the store starts accepting a bare string key again,
  // which is what let a telemetry key or a raw native id stand in for a track key.
  it('refuses a bare string where a TrackKey belongs', () => {
    // @ts-expect-error a raw string must go through trackKey(track) first
    reportPlaybackError('library:trk-1', 'unknown', 'Could not load this track');
    // @ts-expect-error the lookup side is branded too
    const { result } = renderHook(() => usePlaybackErrorFor('library:trk-1'));

    expect(result.current?.message).toBe('Could not load this track');
  });
});

describe('canRetryPlaybackError — which kinds a retry can still rescue', () => {
  it.each([
    ['network', true],
    ['auth', true],
    ['queue_out_of_sync', true],
    ['queue_update_failed', true],
    ['unknown', true],
    ['not_found', false],
    ['decode', false],
  ] as const)('%s is retryable: %s', (kind, canRetry) => {
    expect(canRetryPlaybackError(kind)).toBe(canRetry);
  });

  it('offers a retry when nothing classified the failure', () => {
    expect(canRetryPlaybackError(null)).toBe(true);
  });
});

describe('classifyNativePlaybackError — native PlaybackError codes map to a typed kind', () => {
  it.each([
    ['android-io-network-connection-failed', 'Source error', 'network'],
    ['android-io-network-connection-timeout', 'Source error', 'network'],
    ['android-timeout', 'timed out', 'network'],
    ['ios_not_connected_to_internet', 'offline', 'network'],
    ['android-io-bad-http-status', 'Response code: 401', 'auth'],
    ['android-io-bad-http-status', 'Source error: Response code: 403 for [url]', 'auth'],
    ['android-io-bad-http-status', 'Response code: 410', 'not_found'],
    ['android-io-bad-http-status', 'Response code: 503', 'network'],
    ['android-io-bad-http-status', 'Response code: 408', 'network'],
    ['android-io-bad-http-status', 'Response code: 400', 'unknown'],
    ['android-io-bad-http-status', 'no status here', 'network'],
    ['android-io-file-not-found', 'missing', 'not_found'],
    ['android-parsing-container-malformed', 'bad container', 'decode'],
    ['android-decoding-format-unsupported', 'unsupported', 'decode'],
    ['ios_track_unplayable', 'The track could not be played', 'decode'],
    ['ios_playback_error', 'A playback error occurred', 'unknown'],
    ['', '', 'unknown'],
  ])('%s (%s) is %s', (code, message, kind) => {
    expect(classifyNativePlaybackError(code, message)).toBe(kind);
  });
});

describe('classifyPlaybackFailure — a rejected load maps to a typed kind', () => {
  it('classifies transport and HTTP API failures', () => {
    expect(classifyPlaybackFailure(new NetworkError('timeout', 'timed out'))).toBe('network');
    expect(classifyPlaybackFailure(new ApiError(403, 'forbidden'))).toBe('auth');
    expect(classifyPlaybackFailure(new ApiError(404, 'gone'))).toBe('not_found');
    expect(classifyPlaybackFailure(new ApiError(429, 'slow down'))).toBe('network');
  });

  it('classifies a native rejection by its code', () => {
    const decode = Object.assign(new Error('bad'), { code: 'android-decoding-failed' });
    const badStatus = Object.assign(new Error('Response code: 401'), {
      code: 'android-io-bad-http-status',
    });

    expect(classifyPlaybackFailure(decode)).toBe('decode');
    expect(classifyPlaybackFailure(badStatus)).toBe('auth');
    expect(classifyPlaybackFailure({ code: 'android-timeout' })).toBe('network');
  });

  it('falls back to unknown when nothing identifies the failure', () => {
    expect(classifyPlaybackFailure(new Error('native add failed'))).toBe('unknown');
    expect(classifyPlaybackFailure({ code: 42 })).toBe('unknown');
    expect(classifyPlaybackFailure('not an error')).toBe('unknown');
    expect(classifyPlaybackFailure(null)).toBe('unknown');
  });
});
