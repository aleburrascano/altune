import { _viewForState } from '../state';

function inputs(over: Partial<{ isLoading: boolean; error: Error | null; items: unknown[] }> = {}) {
  return { isLoading: false, error: null as Error | null, items: [1] as unknown[], ...over };
}

describe('_viewForState — precedence and the ready→list remap', () => {
  it('a non-empty, non-error, settled state renders the list, remapping async "ready" to "list"', () => {
    expect(_viewForState(inputs({ items: [1, 2] }))).toBe('list');
  });

  it('an empty settled state renders empty, not list', () => {
    expect(_viewForState(inputs({ items: [] }))).toBe('empty');
  });

  it('an error settled state renders error, not empty and not list', () => {
    expect(_viewForState(inputs({ error: new Error('boom'), items: [] }))).toBe('error');
  });

  it('loading wins over a present error, so the spinner is not pre-empted by a stale error', () => {
    expect(_viewForState(inputs({ isLoading: true, error: new Error('boom'), items: [] }))).toBe(
      'loading',
    );
  });

  it('error wins over emptiness, so a failed load is not mistaken for an empty library', () => {
    expect(_viewForState(inputs({ isLoading: false, error: new Error('boom'), items: [] }))).toBe(
      'error',
    );
  });

  it('emptiness is decided by an empty items array while loading is done and there is no error', () => {
    expect(_viewForState(inputs({ items: [] }))).toBe('empty');
    expect(_viewForState(inputs({ items: ['t'] }))).toBe('list');
  });
});
