import {
  GROUP_PAGE_SIZE,
  nextGroupPageOffset,
  pagedListControls,
} from '@features/library/domain/listPaging';

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

function build(over: Partial<Parameters<typeof pagedListControls>[0]> = {}) {
  const fetchNextPage = jest.fn();
  const refetch = jest.fn();
  const controls = pagedListControls({
    hasNextPage: true,
    isFetchingNextPage: false,
    isFetchNextPageError: false,
    fetchNextPage,
    refetch,
    ...over,
  });
  return { controls, fetchNextPage, refetch };
}

describe('pagedListControls', () => {
  it('fetches the next page when one exists and nothing is in flight', () => {
    const { controls, fetchNextPage } = build();
    controls.onEndReached();
    expect(fetchNextPage).toHaveBeenCalledTimes(1);
  });

  it.each([{ hasNextPage: false }, { isFetchingNextPage: true }, { isFetchNextPageError: true }])(
    'does not fetch when %o',
    (over) => {
      const { controls, fetchNextPage } = build(over);
      controls.onEndReached();
      expect(fetchNextPage).not.toHaveBeenCalled();
    },
  );

  it('refetch calls through', () => {
    const { controls, refetch } = build();
    controls.refetch();
    expect(refetch).toHaveBeenCalledTimes(1);
  });
});
