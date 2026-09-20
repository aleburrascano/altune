import { ApiError, ContractError, NetworkError } from '@shared/errors';
import { RETRY_TAIL } from '@shared/lib/describeError';

import { _viewForState, classifyLibraryError, failureTail } from '../state';

function inputs(over: Partial<{ isLoading: boolean; error: Error | null; items: unknown[] }> = {}) {
  return { isLoading: false, error: null as Error | null, items: [1] as unknown[], ...over };
}

const viewOf = (over: Parameters<typeof inputs>[0]) => _viewForState(inputs(over)).view;

describe('_viewForState — precedence and the ready→list remap', () => {
  it('a non-empty, non-error, settled state renders the list, remapping async "ready" to "list"', () => {
    expect(viewOf({ items: [1, 2] })).toBe('list');
  });

  it('an empty settled state renders empty, not list', () => {
    expect(viewOf({ items: [] })).toBe('empty');
  });

  it('an error settled state renders error, not empty and not list', () => {
    expect(viewOf({ error: new Error('boom'), items: [] })).toBe('error');
  });

  it('loading wins over a present error, so the spinner is not pre-empted by a stale error', () => {
    expect(_viewForState(inputs({ isLoading: true, error: new Error('boom'), items: [] }))).toEqual(
      { view: 'loading', failure: null },
    );
  });

  it('error wins over emptiness, so a failed load is not mistaken for an empty library', () => {
    expect(viewOf({ isLoading: false, error: new Error('boom'), items: [] })).toBe('error');
  });

  it('emptiness is decided by an empty items array while loading is done and there is no error', () => {
    expect(viewOf({ items: [] })).toBe('empty');
    expect(viewOf({ items: ['t'] })).toBe('list');
  });
});

// #795: the error state was only ever `Boolean(error)`, so an offline load and an
// expired session produced the identical 'error' state and no caller could tell them apart.
describe('_viewForState — the error state carries why the load failed', () => {
  it('two different causes for the same failed load produce distinguishable states', () => {
    const offline = _viewForState(inputs({ error: new NetworkError('transport', 'x'), items: [] }));
    const expired = _viewForState(inputs({ error: new ApiError(401, 'x'), items: [] }));

    expect(offline).toEqual({ view: 'error', failure: 'network' });
    expect(expired).toEqual({ view: 'error', failure: 'auth' });
  });

  it('a settled, error-free state reports no failure', () => {
    expect(_viewForState(inputs({ items: [] }))).toEqual({ view: 'empty', failure: null });
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
