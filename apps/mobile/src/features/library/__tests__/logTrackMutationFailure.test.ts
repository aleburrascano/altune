// #1703: this logger sits above `apiFetch`'s redaction boundary, so it must carry
// the same shape — never the caught error's message or stack, never a query string.

import { ApiError, NetworkError } from '@shared/errors';
import { asTrackId } from '@shared/api-client/ids';

import { logTrackMutationFailure } from '../hooks/logTrackMutationFailure';

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

/**
 * What a console would render. `message` and `stack` are non-enumerable, so a
 * plain `JSON.stringify` of a logged Error yields `{}` and would hide the leak
 * this file exists to catch.
 */
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
    // An impostor: a thrown object wearing the fields of an ApiError.
    logTrackMutationFailure('delete track', deleteEndpoint, trackId, {
      status: 500,
      code: SECRET,
      correlationId: SECRET,
    });
    // `Promise.reject()` with no reason.
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
