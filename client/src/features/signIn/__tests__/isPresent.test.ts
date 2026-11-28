import { isPresent } from '@features/signIn/core/isPresent';

describe('isPresent', () => {
  it.each([[null], [undefined], ['']])('treats %p as absent', (value) => {
    expect(isPresent(value)).toBe(false);
  });

  it.each([['a'], [' '], ['0']])('treats %p as present', (value) => {
    expect(isPresent(value)).toBe(true);
  });
});
