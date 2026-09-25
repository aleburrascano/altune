import { pagedListControls } from '../hooks/pagedListControls';

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

  it.each([
    { hasNextPage: false },
    { isFetchingNextPage: true },
    { isFetchNextPageError: true },
  ])('does not fetch when %o', (over) => {
    const { controls, fetchNextPage } = build(over);
    controls.onEndReached();
    expect(fetchNextPage).not.toHaveBeenCalled();
  });

  it('refetch calls through', () => {
    const { controls, refetch } = build();
    controls.refetch();
    expect(refetch).toHaveBeenCalledTimes(1);
  });
});
