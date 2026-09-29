import { ApiError, ContractError, NetworkError } from '@shared/errors';
import { RETRY_TAIL } from '@shared/lib/describeError';

import { classifyLibraryError, failureTail, librarySection } from '../state';

function inputs(
  over: Partial<{
    isLoading: boolean;
    error: Error | null;
    count: number;
    showEmpty: boolean;
  }> = {},
) {
  return { isLoading: false, error: null as Error | null, count: 1, showEmpty: true, ...over };
}

const viewOf = (over: Parameters<typeof inputs>[0]) => librarySection(inputs(over));

describe('librarySection — precedence', () => {
  it('a non-empty, non-error, settled state is ready', () => {
    expect(viewOf({ count: 2 })).toBe('ready');
  });

  it('an empty settled state that may show empty is empty, not ready', () => {
    expect(viewOf({ count: 0 })).toBe('empty');
  });

  it('an empty settled state that may not show empty stays ready', () => {
    expect(viewOf({ count: 0, showEmpty: false })).toBe('ready');
  });

  it('an error settled state is error, not empty and not ready', () => {
    expect(viewOf({ error: new Error('boom'), count: 0 })).toBe('error');
  });

  it('loading wins over a present error, so the spinner is not pre-empted by a stale error', () => {
    expect(viewOf({ isLoading: true, error: new Error('boom'), count: 0 })).toBe('loading');
  });

  it('error wins over emptiness, so a failed load is not mistaken for an empty library', () => {
    expect(viewOf({ error: new Error('boom'), count: 0, showEmpty: true })).toBe('error');
  });

  it('emptiness needs count 0 and showEmpty once loading is done and there is no error', () => {
    expect(viewOf({ count: 0 })).toBe('empty');
    expect(viewOf({ count: 1 })).toBe('ready');
  });
});

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
