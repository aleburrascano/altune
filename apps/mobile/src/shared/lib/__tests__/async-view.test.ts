import { asyncView } from '../async-view';
import type { AsyncInputs, AsyncView } from '../async-view';

describe('asyncView — precedence over the full 2^3 input space', () => {
  it.each<[AsyncInputs, AsyncView]>([
    [{ isLoading: false, isError: false, isEmpty: false }, 'ready'],
    [{ isLoading: false, isError: false, isEmpty: true }, 'empty'],
    [{ isLoading: false, isError: true, isEmpty: false }, 'error'],
    [{ isLoading: false, isError: true, isEmpty: true }, 'error'],
    [{ isLoading: true, isError: false, isEmpty: false }, 'loading'],
    [{ isLoading: true, isError: false, isEmpty: true }, 'loading'],
    [{ isLoading: true, isError: true, isEmpty: false }, 'loading'],
    [{ isLoading: true, isError: true, isEmpty: true }, 'loading'],
  ])('%j -> %s', (inputs, expected) => {
    expect(asyncView(inputs)).toBe(expected);
  });

  it('loading always wins over a simultaneous error, regardless of emptiness', () => {
    expect(asyncView({ isLoading: true, isError: true, isEmpty: false })).toBe('loading');
    expect(asyncView({ isLoading: true, isError: true, isEmpty: true })).toBe('loading');
  });

  it('error wins over empty when neither is loading', () => {
    expect(asyncView({ isLoading: false, isError: true, isEmpty: true })).toBe('error');
  });
});
