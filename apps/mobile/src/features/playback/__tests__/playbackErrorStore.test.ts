import { renderHook } from '@testing-library/react-native';

import { ApiError, NetworkError } from '@shared/api-client/errors';

import {
  classifyNativePlaybackError,
  classifyPlaybackFailure,
  clearPlaybackError,
  reportPlaybackError,
  usePlaybackErrorFor,
  usePlaybackErrorStore,
} from '../playbackErrorStore';

afterEach(() => {
  usePlaybackErrorStore.getState().clear();
});

describe('playbackErrorStore — recording and clearing a track error', () => {
  it('records the failing key, its kind and its message', () => {
    reportPlaybackError('library:trk-1', 'network', 'Could not load this track');

    expect(usePlaybackErrorStore.getState().key).toBe('library:trk-1');
    expect(usePlaybackErrorStore.getState().kind).toBe('network');
    expect(usePlaybackErrorStore.getState().message).toBe('Could not load this track');
  });

  it('clears the key, the kind and the message', () => {
    reportPlaybackError('library:trk-1', 'unknown', 'Could not load this track');

    clearPlaybackError();

    expect(usePlaybackErrorStore.getState().key).toBeNull();
    expect(usePlaybackErrorStore.getState().kind).toBeNull();
    expect(usePlaybackErrorStore.getState().message).toBeNull();
  });
});

describe('usePlaybackErrorFor — the message a given track should show', () => {
  it('returns the message when the reported key matches', () => {
    reportPlaybackError('library:trk-1', 'unknown', 'Could not load this track');

    const { result } = renderHook(() => usePlaybackErrorFor('library:trk-1'));

    expect(result.current).toBe('Could not load this track');
  });

  it('returns null for a different track than the one that failed', () => {
    reportPlaybackError('library:trk-1', 'unknown', 'Could not load this track');

    const { result } = renderHook(() => usePlaybackErrorFor('library:trk-2'));

    expect(result.current).toBeNull();
  });

  it('returns null when the queried key is null', () => {
    reportPlaybackError('library:trk-1', 'unknown', 'Could not load this track');

    const { result } = renderHook(() => usePlaybackErrorFor(null));

    expect(result.current).toBeNull();
  });
});

describe('reportPlaybackError — secrets in a native error message are redacted before storing', () => {
  const SIGNED_URL =
    'https://audio.altune.example/tracks/trk-1.m4a?X-Amz-Credential=AKIAEXAMPLE&X-Amz-Signature=deadbeefcafe&token=s3cr3t-token';

  function storedMessage(message: string): string | null {
    reportPlaybackError('library:trk-1', 'unknown', message);
    return usePlaybackErrorStore.getState().message;
  }

  it('redacts a whole signed URL, including its query-string token, in what the UI reads', () => {
    reportPlaybackError(
      'library:trk-1',
      'unknown',
      `Source error: Response code: 403 for ${SIGNED_URL}`,
    );

    const { result } = renderHook(() => usePlaybackErrorFor('library:trk-1'));

    expect(result.current).toBe('Source error: Response code: 403 for [redacted url]');
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
    usePlaybackErrorStore.getState().report('library:trk-1', 'unknown', `failed ${SIGNED_URL}`);

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
