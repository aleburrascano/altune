import { ApiError, ContractError, NetworkError } from '@shared/failure/errors';
import { RETRY_TAIL } from '@shared/failure/describeError';
import {
  classifyLibraryError,
  failureTail,
  libraryFailureAlert,
  logTrackMutationFailure,
  trackNotFoundAlert,
} from '@features/library/domain/libraryFailure';
import { asTrackId, type TrackId } from '@shared/apiClient/ids';

describe('classifyLibraryError — one boundary, typed errors only', () => {
  it.each([
    [new NetworkError('timeout', 'timed out'), 'network'],
    [new NetworkError('transport', 'unreachable'), 'network'],
    [Object.assign(new Error('fetch failed'), { name: 'AuthRetryableFetchError' }), 'network'],
    [new ApiError(401, 'unauthorized'), 'auth'],
    [new ApiError(403, 'forbidden'), 'auth'],
    [new ApiError(404, 'not found'), 'not-found'],
    [new ApiError(410, 'gone'), 'not-found'],
    [new ApiError(429, 'slow down'), 'server'],
    [new ApiError(500, 'internal'), 'server'],
    [new ApiError(503, 'unavailable'), 'server'],
    [new ContractError('GET /v1/tracks', 'bad shape'), 'server'],
    [new ApiError(409, 'conflict'), 'unknown'],
    [new ApiError(400, 'bad request'), 'unknown'],
    [null, 'unknown'],
    ['boom', 'unknown'],
  ])('%p is classified as %s', (error, failure) => {
    expect(classifyLibraryError(error)).toBe(failure);
  });

  it('never classifies by message text: a plain Error that mentions the network is unknown', () => {
    expect(classifyLibraryError(new Error('Network request failed'))).toBe('unknown');
    expect(classifyLibraryError(new Error('404 not found'))).toBe('unknown');
  });
});

describe('failureTail — the closing ask of a failure Alert', () => {
  it('asks to sign in for an auth failure and to try again for everything else', () => {
    expect(failureTail('auth')).toBe('Sign in again, then retry.');
    for (const failure of ['network', 'not-found', 'server', 'unknown'] as const) {
      expect(failureTail(failure)).toBe(RETRY_TAIL);
    }
  });
});

describe('libraryFailureAlert — the payload carries the surface, the title as given, the lead then the closing ask', () => {
  it.each([
    ['auth', 'Could not remove the track. Sign in again, then retry.'],
    ['network', 'Could not remove the track. Please try again.'],
    ['server', 'Could not remove the track. Please try again.'],
    ['not-found', 'Could not remove the track. Please try again.'],
    ['unknown', 'Could not remove the track. Please try again.'],
  ] as const)('a %s failure gives "%s"', (failure, message) => {
    expect(
      libraryFailureAlert(
        'library.delete_track',
        'Delete failed',
        'Could not remove the track.',
        failure,
      ),
    ).toEqual({ surface: 'library.delete_track', title: 'Delete failed', message });
  });
});

const SECRET = 'bearer-eyJ-private-search-term';
const trackId = asTrackId('t1');
const deleteEndpoint = () => 'DELETE /v1/tracks/t1';

let warnSpy: jest.SpyInstance;

beforeEach(() => {
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  warnSpy.mockRestore();
});

function loggedText(): string {
  return JSON.stringify(warnSpy.mock.calls, (_key, value: unknown) =>
    value instanceof Error ? `${value.name}: ${value.message} ${value.stack ?? ''}` : value,
  );
}

describe('logTrackMutationFailure carries triage fields and nothing the error dragged in', () => {
  it('omits the message of the caught ApiError, which can hold a server message', () => {
    logTrackMutationFailure('delete track', deleteEndpoint, trackId, new ApiError(500, SECRET));

    expect(loggedText()).not.toContain(SECRET);
  });

  it('omits the message and stack of a plain Error', () => {
    logTrackMutationFailure('delete track', deleteEndpoint, trackId, new Error(SECRET));

    expect(loggedText()).not.toContain(SECRET);
  });

  it('logs status, code, failure class and correlation id for an ApiError', () => {
    const error = new ApiError(409, 'conflict', 'track_locked', 'corr-1');

    logTrackMutationFailure('delete track', deleteEndpoint, trackId, error);

    expect(warnSpy).toHaveBeenCalledWith('[library] delete track failed', {
      trackId: 't1',
      endpoint: 'DELETE /v1/tracks/t1',
      status: 409,
      code: 'track_locked',
      failure: 'unknown',
      correlationId: 'corr-1',
    });
  });

  it('logs the failure class and correlation id for a NetworkError, which has no status', () => {
    const error = new NetworkError('timeout', `API ${SECRET} timed out`, 'corr-2');

    logTrackMutationFailure('retry acquisition', () => 'POST /v1/tracks/t1/retry', trackId, error);

    expect(warnSpy).toHaveBeenCalledWith('[library] retry acquisition failed', {
      trackId: 't1',
      endpoint: 'POST /v1/tracks/t1/retry',
      failure: 'network',
      correlationId: 'corr-2',
    });
  });

  it('logs a plain Error as an unclassified failure with no status or correlation id', () => {
    logTrackMutationFailure('delete track', deleteEndpoint, trackId, new Error('boom'));

    expect(warnSpy).toHaveBeenCalledWith('[library] delete track failed', {
      trackId: 't1',
      endpoint: 'DELETE /v1/tracks/t1',
      failure: 'unknown',
    });
  });

  it('reads no field off a thrown string, object or nothing at all', () => {
    const unclassified = {
      trackId: 't1',
      endpoint: 'DELETE /v1/tracks/t1',
      failure: 'unknown',
    };

    logTrackMutationFailure('delete track', deleteEndpoint, trackId, SECRET);
    logTrackMutationFailure('delete track', deleteEndpoint, trackId, {
      status: 500,
      code: SECRET,
      correlationId: SECRET,
    });
    logTrackMutationFailure('delete track', deleteEndpoint, trackId, undefined);

    expect(loggedText()).not.toContain(SECRET);
    expect(warnSpy).toHaveBeenNthCalledWith(1, '[library] delete track failed', unclassified);
    expect(warnSpy).toHaveBeenNthCalledWith(2, '[library] delete track failed', unclassified);
    expect(warnSpy).toHaveBeenNthCalledWith(3, '[library] delete track failed', unclassified);
  });

  it('strips a query string an endpoint builder puts in the path', () => {
    logTrackMutationFailure(
      'delete track',
      (id) => `DELETE /v1/tracks/${id}?q=${SECRET}`,
      trackId,
      new ApiError(500, 'internal'),
    );

    expect(loggedText()).not.toContain(SECRET);
    expect(warnSpy).toHaveBeenCalledWith(
      '[library] delete track failed',
      expect.objectContaining({ endpoint: 'DELETE /v1/tracks/t1' }),
    );
  });
});

describe('trackNotFoundAlert', () => {
  it('builds the exact payload for the track', () => {
    expect(trackNotFoundAlert('track-1' as TrackId)).toEqual({
      surface: 'library.track_not_found',
      title: 'Track not found',
      message: 'This track is no longer in your library.',
      trackId: 'track-1',
    });
  });

  it('carries the id it was given', () => {
    expect(trackNotFoundAlert('track-2' as TrackId)).toEqual({
      surface: 'library.track_not_found',
      title: 'Track not found',
      message: 'This track is no longer in your library.',
      trackId: 'track-2',
    });
  });
});
