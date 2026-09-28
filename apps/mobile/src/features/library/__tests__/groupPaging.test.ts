import { GROUP_PAGE_SIZE, nextGroupPageOffset } from '../groupPaging';

describe('nextGroupPageOffset', () => {
  it('advances past a full default-sized page', () => {
    expect(GROUP_PAGE_SIZE).toBe(50);
    expect(nextGroupPageOffset(50, 0)).toBe(50);
  });

  it('ends the list when a default-sized page comes back short', () => {
    expect(nextGroupPageOffset(49, 0)).toBeUndefined();
  });

  it('advances past a full page at a custom page size', () => {
    expect(nextGroupPageOffset(5, 10, 5)).toBe(15);
  });
});
