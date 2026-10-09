import { act, renderHook } from '@testing-library/react-native';

import { NetworkError } from '@shared/errors';
import { recordEvent } from '@shared/telemetry/recordEvent';
import { asTrackId } from '@shared/api-client/ids';
import { trackKey } from '@shared/playback/trackKey';

import { expectType, type IsAssignable, type Not } from '@testing/typeAssertions';

import {
  clearPlaybackError,
  reportLoadFailure,
  reportPlaybackError,
  usePlaybackErrorFor,
  usePlaybackErrorStore,
} from '@features/playback/hooks/usePlaybackErrorStore';
import {
  _resetPlaybackHealthForTest,
  flushPlaybackHealth,
} from '@features/playback/domain/playbackHealth';

import { libraryTrack } from '@features/playback/__tests__/fixtures';

jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

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
  it('refuses a bare string where a TrackKey belongs', () => {
    expectType<Not<IsAssignable<string, Parameters<typeof reportPlaybackError>[0]>>>();
    expectType<Not<IsAssignable<string, Parameters<typeof usePlaybackErrorFor>[0]>>>();
  });
});

describe('reportLoadFailure — one log line and one health count per load failure', () => {
  let warn: jest.SpyInstance;

  beforeEach(() => {
    _resetPlaybackHealthForTest();
    (recordEvent as jest.Mock).mockReset().mockResolvedValue(undefined);
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    warn.mockRestore();
  });

  it('logs one redacted [playback] warn and still sets the store', () => {
    reportLoadFailure(libraryTrack(), new Error('boom https://x.test/a?token=abc.def'), 'Custom');

    expect(warn).toHaveBeenCalledTimes(1);
    expect(warn).toHaveBeenCalledWith('[playback] track load failed', {
      kind: 'unknown',
      error: { kind: 'unknown', message: expect.not.stringMatching(/abc\.def/) },
    });
    expect(usePlaybackErrorStore.getState().key).toBe(FAILED_KEY);
    expect(usePlaybackErrorStore.getState().kind).toBe('unknown');
    expect(usePlaybackErrorStore.getState().message).toBe('Custom');
  });

  it('counts the classified kind once in the playback health tally', () => {
    reportLoadFailure(libraryTrack(), new NetworkError('timeout', 'timed out'));
    flushPlaybackHealth();

    expect(recordEvent).toHaveBeenCalledTimes(1);
    expect(recordEvent).toHaveBeenCalledWith({
      type: 'playback_health',
      payload: expect.objectContaining({ playback_failed_network: 1 }),
    });
  });
});
