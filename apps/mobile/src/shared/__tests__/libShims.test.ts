import { ApiError } from '@shared/errors';
import * as oldDescribe from '@shared/lib/describeError';
import * as oldNetwork from '@shared/lib/isNetworkError';
import * as newDescribe from '@shared/failure/describeError';
import * as newNetwork from '@shared/failure/isNetworkError';

describe('lib shims re-export the failure modules', () => {
  it('exposes the same objects through both paths', () => {
    expect(oldDescribe.describeError).toBe(newDescribe.describeError);
    expect(oldDescribe.RETRY_TAIL).toBe(newDescribe.RETRY_TAIL);
    expect(oldNetwork.isNetworkError).toBe(newNetwork.isNetworkError);
  });

  it('treats undefined as not a network error and a timeout as one', () => {
    for (const isNetworkError of [oldNetwork.isNetworkError, newNetwork.isNetworkError]) {
      expect(isNetworkError(undefined)).toBe(false);
      expect(isNetworkError(new Error('timeout'))).toBe(true);
    }
  });

  it('refuses an ApiError that looks like a network failure', () => {
    const err = new ApiError(400, 'network down');
    expect(oldNetwork.isNetworkError(err)).toBe(false);
    expect(newNetwork.isNetworkError(err)).toBe(false);
    expect(oldDescribe.describeError(err).title).not.toBe('No connection');
    expect(newDescribe.describeError(err).title).not.toBe('No connection');
  });

  it('agrees on one Error whose message changes between calls', () => {
    const err = new Error('boom');
    expect(oldNetwork.isNetworkError(err)).toBe(false);
    expect(newNetwork.isNetworkError(err)).toBe(false);
    err.message = 'fetch failed';
    expect(oldNetwork.isNetworkError(err)).toBe(true);
    expect(newNetwork.isNetworkError(err)).toBe(true);
  });
});
