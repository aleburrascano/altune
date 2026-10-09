import { ApiError, NetworkError } from '@shared/failure/errors';

import {
  classifyNativePlaybackError,
  classifyPlaybackFailure,
} from '@features/playback/domain/playbackError';

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
