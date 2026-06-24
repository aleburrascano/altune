import { asyncView } from '../async-view';

describe('asyncView', () => {
  it('returns ready when nothing is pending', () => {
    expect(asyncView({ isLoading: false, isError: false, isEmpty: false })).toBe('ready');
  });

  it('returns loading first', () => {
    expect(asyncView({ isLoading: true, isError: true, isEmpty: true })).toBe('loading');
  });

  it('returns error over empty', () => {
    expect(asyncView({ isLoading: false, isError: true, isEmpty: true })).toBe('error');
  });

  it('returns empty over ready', () => {
    expect(asyncView({ isLoading: false, isError: false, isEmpty: true })).toBe('empty');
  });
});
