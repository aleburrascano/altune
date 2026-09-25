import { ApiError, NetworkError } from '@shared/api-client';
import { failureCopyForAction } from '../failureCopyForAction';

describe('failureCopyForAction', () => {
  it('maps network, auth, server and unknown failures to distinct copy', () => {
    const copy = [
      new NetworkError('timeout', 't'),
      new ApiError(401, 'x'),
      new ApiError(403, 'x'),
      new ApiError(502, 'x'),
      new ApiError(404, 'x'),
      new Error('boom'),
    ].map(failureCopyForAction);
    expect(copy).toEqual([
      'Could not reach the server — check your connection and try again.',
      'Your session has expired — sign in again and retry.',
      'Your session has expired — sign in again and retry.',
      'The server had a problem — try again in a few minutes.',
      'Something went wrong — try again.',
      'Something went wrong — try again.',
    ]);
  });
});
