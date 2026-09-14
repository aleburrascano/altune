import { renderHook } from '@testing-library/react-native';

import {
  clearPlaybackError,
  reportPlaybackError,
  usePlaybackErrorFor,
  usePlaybackErrorStore,
} from '../playbackErrorStore';

afterEach(() => {
  usePlaybackErrorStore.getState().clear();
});

describe('playbackErrorStore — recording and clearing a track error', () => {
  it('records the failing key and its message', () => {
    reportPlaybackError('library:trk-1', 'Could not load this track');

    expect(usePlaybackErrorStore.getState().key).toBe('library:trk-1');
    expect(usePlaybackErrorStore.getState().message).toBe('Could not load this track');
  });

  it('clears both the key and the message', () => {
    reportPlaybackError('library:trk-1', 'Could not load this track');

    clearPlaybackError();

    expect(usePlaybackErrorStore.getState().key).toBeNull();
    expect(usePlaybackErrorStore.getState().message).toBeNull();
  });
});

describe('usePlaybackErrorFor — the message a given track should show', () => {
  it('returns the message when the reported key matches', () => {
    reportPlaybackError('library:trk-1', 'Could not load this track');

    const { result } = renderHook(() => usePlaybackErrorFor('library:trk-1'));

    expect(result.current).toBe('Could not load this track');
  });

  it('returns null for a different track than the one that failed', () => {
    reportPlaybackError('library:trk-1', 'Could not load this track');

    const { result } = renderHook(() => usePlaybackErrorFor('library:trk-2'));

    expect(result.current).toBeNull();
  });

  it('returns null when the queried key is null', () => {
    reportPlaybackError('library:trk-1', 'Could not load this track');

    const { result } = renderHook(() => usePlaybackErrorFor(null));

    expect(result.current).toBeNull();
  });
});

describe('reportPlaybackError — secrets in a native error message are redacted before storing', () => {
  const SIGNED_URL =
    'https://audio.altune.example/tracks/trk-1.m4a?X-Amz-Credential=AKIAEXAMPLE&X-Amz-Signature=deadbeefcafe&token=s3cr3t-token';

  function storedMessage(message: string): string | null {
    reportPlaybackError('library:trk-1', message);
    return usePlaybackErrorStore.getState().message;
  }

  it('redacts a whole signed URL, including its query-string token, in what the UI reads', () => {
    reportPlaybackError('library:trk-1', `Source error: Response code: 403 for ${SIGNED_URL}`);

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
    usePlaybackErrorStore.getState().report('library:trk-1', `failed ${SIGNED_URL}`);

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
